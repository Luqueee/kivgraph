/**
 * The candidate hero animations, one function each, mounted by
 * `/hero-anim/<name>/` so they are judged on the real hero rather than on a
 * description of them.
 *
 * Every variant returns a `replay`, which is what the preview's button calls.
 * Entry animations restart; the two interaction variants re-arm their handlers
 * instead, because there is nothing to replay about following a cursor.
 *
 * The LCP rule from `landing/AGENTS.md` holds here too: the title may animate,
 * but never behind a delay. A zero-delay fade measured `88 ms` against a
 * `76 ms` baseline; the same fade behind `600 ms` of delay measured `1112 ms`.
 *
 * Delete this module with `src/pages/hero-anim/` once a variant is chosen.
 */
import { gsap } from "gsap";
import { SplitText } from "gsap/SplitText";

export type VariantName =
  | "cascade"
  | "ignite"
  | "words"
  | "pointer"
  | "magnetic"
  | "full";

export interface Variant {
  readonly label: string;
  readonly summary: string;
  /** What the reader has to do, when a button cannot show it. */
  readonly interaction: string | null;
  readonly cost: string;
}

export const VARIANTS: Record<VariantName, Variant> = {
  cascade: {
    label: "1 - staggered entrance",
    summary:
      "The title enters at zero delay; everything else follows it 70ms apart. The order is the reading order.",
    interaction: null,
    cost: "LCP 88ms against a 76ms baseline.",
  },
  ignite: {
    label: "3 - ignition",
    summary:
      "The grid fades up and the sheen makes one fast pass before settling into its 15s loop. The page reads as powering on.",
    interaction: null,
    cost: "Backdrop only. It cannot touch LCP: the title is untouched.",
  },
  words: {
    label: "5 - title, word by word",
    summary:
      "Each word rises out of a clipped line. Transform only, no fade, because a transform-only stagger measured free.",
    interaction: null,
    cost: "Adds SplitText, 30KB raw. Measure the stagger before believing it.",
  },
  pointer: {
    label: "2 - pointer-reactive grid",
    summary:
      "A halo follows the cursor and lights the grid under it. A translated blurred element, so it stays on the compositor.",
    interaction: "Move the pointer across the band.",
    cost: "Runs after LCP. No layout property is touched.",
  },
  magnetic: {
    label: "4 - magnetic actions",
    summary:
      "The two buttons lean toward the cursor and lift slightly while it is over them.",
    interaction: "Hover the two buttons.",
    cost: "Transform only, and it reverts on pointerleave.",
  },
  full: {
    label: "1 + 3 + 4 + 2 - the recommendation",
    summary:
      "The staggered entrance, the ignition, the magnetic buttons and the pointer halo, together, in the order they would ship.",
    interaction: "Move the pointer, then hover the buttons.",
    cost: "The union of the four above.",
  },
};

/** Reading order, which is also the order the entrance uses. */
const ORDER = ["title", "lede", "actions", "install", "card", "facts"] as const;

/** How long the sheen takes when it is making its entrance pass. */
const IGNITION_SHEEN = 1.1;

function itemsOf(root: ParentNode): HTMLElement[] {
  return ORDER.map((name) =>
    root.querySelector<HTMLElement>(`[data-hero-item="${name}"]`),
  ).filter((node): node is HTMLElement => node !== null);
}

/**
 * The staggered entrance. The title is its own tween at zero delay and the rest
 * are one staggered tween beside it, which is the whole point: a single
 * `stagger` across all six would put the title in the queue behind nothing and
 * still pay for the ones after it.
 */
function cascade(root: HTMLElement): gsap.core.Timeline {
  const [title, ...rest] = itemsOf(root);
  const timeline = gsap.timeline();

  if (title !== undefined) {
    timeline.from(title, {
      opacity: 0,
      y: 20,
      duration: 0.7,
      ease: "power3.out",
    });
  }
  if (rest.length > 0) {
    timeline.from(
      rest,
      { opacity: 0, y: 18, duration: 0.55, ease: "power2.out", stagger: 0.07 },
      0.12,
    );
  }
  return timeline;
}

/**
 * The backdrop powering on. The sheen is a `::after` with a CSS loop, which no
 * tween can reach, so the hero declares its duration as `--sheen-duration` and
 * this shortens it for one pass and then hands it back. Removing the property
 * rather than resetting it is what returns the element to the stylesheet's own
 * value instead of a copy of it.
 */
function ignite(root: HTMLElement): gsap.core.Timeline {
  const field = root.querySelector<HTMLElement>("[data-hero-field]");
  const timeline = gsap.timeline();
  if (field === null) {
    return timeline;
  }

  return timeline
    .set(field, { "--sheen-duration": `${IGNITION_SHEEN}s` })
    .from(field, { opacity: 0, scale: 1.05, duration: 0.9, ease: "power2.out" })
    .call(
      () => field.style.removeProperty("--sheen-duration"),
      undefined,
      IGNITION_SHEEN,
    );
}

/**
 * The title, word by word, transform only. `SplitText` wraps each line so the
 * words have an edge to rise out of; the wrapper is an inline-block with
 * `overflow: hidden`, which occupies the box the line already had.
 */
function words(root: HTMLElement): gsap.core.Timeline {
  const title = root.querySelector<HTMLElement>('[data-hero-item="title"]');
  const timeline = gsap.timeline();
  if (title === null) {
    return timeline;
  }

  gsap.registerPlugin(SplitText);
  const split = new SplitText(title, {
    type: "lines,words",
    linesClass: "hero-line",
  });

  return timeline.from(split.words, {
    yPercent: 120,
    duration: 0.8,
    ease: "power4.out",
    stagger: 0.035,
  });
}

/** The halo that follows the pointer. Returns its own teardown. */
function pointer(root: HTMLElement): () => void {
  const halo = document.createElement("div");
  halo.className = "hero-halo";
  halo.setAttribute("aria-hidden", "true");
  root.append(halo);

  const toX = gsap.quickTo(halo, "x", { duration: 0.5, ease: "power3.out" });
  const toY = gsap.quickTo(halo, "y", { duration: 0.5, ease: "power3.out" });

  const move = (event: PointerEvent): void => {
    const box = root.getBoundingClientRect();
    toX(event.clientX - box.left);
    toY(event.clientY - box.top);
  };
  const show = (): void => {
    gsap.to(halo, { opacity: 1, duration: 0.4 });
  };
  const hide = (): void => {
    gsap.to(halo, { opacity: 0, duration: 0.4 });
  };

  root.addEventListener("pointermove", move);
  root.addEventListener("pointerenter", show);
  root.addEventListener("pointerleave", hide);

  return () => {
    root.removeEventListener("pointermove", move);
    root.removeEventListener("pointerenter", show);
    root.removeEventListener("pointerleave", hide);
    halo.remove();
  };
}

/** The two buttons leaning toward the cursor. Returns its own teardown. */
function magnetic(root: HTMLElement): () => void {
  const targets = Array.from(
    root.querySelectorAll<HTMLElement>('[data-hero-item="actions"] a'),
  );
  const teardowns: Array<() => void> = [];

  for (const target of targets) {
    const move = (event: PointerEvent): void => {
      const box = target.getBoundingClientRect();
      gsap.to(target, {
        x: ((event.clientX - box.left) / box.width - 0.5) * 8,
        y: ((event.clientY - box.top) / box.height - 0.5) * 6,
        scale: 1.03,
        duration: 0.35,
        ease: "power3.out",
      });
    };
    const reset = (): void => {
      gsap.to(target, {
        x: 0,
        y: 0,
        scale: 1,
        duration: 0.45,
        ease: "power3.out",
      });
    };

    target.addEventListener("pointermove", move);
    target.addEventListener("pointerleave", reset);
    teardowns.push(() => {
      target.removeEventListener("pointermove", move);
      target.removeEventListener("pointerleave", reset);
      gsap.set(target, { clearProps: "transform" });
    });
  }

  return () => {
    for (const teardown of teardowns) {
      teardown();
    }
  };
}

export interface Mounted {
  /** Runs the entrance again. Interaction-only variants have nothing to run. */
  replay: () => void;
  hasEntrance: boolean;
}

/** Mounts one variant on a hero section. */
export function mountVariant(root: HTMLElement, variant: VariantName): Mounted {
  const timelines: gsap.core.Timeline[] = [];

  if (variant === "cascade" || variant === "full") {
    timelines.push(cascade(root));
  }
  if (variant === "ignite" || variant === "full") {
    timelines.push(ignite(root));
  }
  if (variant === "words") {
    timelines.push(words(root));
  }
  if (variant === "pointer" || variant === "full") {
    pointer(root);
  }
  if (variant === "magnetic" || variant === "full") {
    magnetic(root);
  }

  return {
    hasEntrance: timelines.length > 0,
    replay: () => {
      for (const timeline of timelines) {
        timeline.restart();
      }
    },
  };
}
