/**
 * The landing page's motion layer: scroll-driven reveals and one parallax on
 * the hero's backdrop.
 *
 * Three rules hold this together, and each of them is a measurement rather than
 * a preference:
 * - **Nothing above the fold waits its turn.** The `h1` is the page's LCP
 *   candidate, and what moves that metric is the *delay*, not the fade: a
 *   zero-delay fade measured `88 ms` against a `76 ms` baseline, while the same
 *   fade behind a `600 ms` delay measured `1112 ms`. So the hero may animate,
 *   but it may never be staggered after something else. The numbers and the
 *   probe that produced them are in `landing/AGENTS.md`.
 * - **The start state is set by GSAP, never in CSS.** `gsap.from` applies it,
 *   so a blocked or failed bundle leaves every section fully visible instead of
 *   hiding the page behind a script. No element rests at `opacity: 0`: the only
 *   such declaration in the stylesheets is the keyframe that blinks the
 *   transcript's caret.
 * - **Only `opacity` and `transform` are animated.** Neither reads back into
 *   layout, so no reveal can contribute to Cumulative Layout Shift.
 *
 * Content is server-rendered and present in the HTML before any of this runs;
 * the animation is decoration over markup a crawler already has.
 */
import { gsap } from "gsap";
import { ScrollTrigger } from "gsap/ScrollTrigger";

/** How far a revealed block travels, in pixels. Small enough to read as arrival. */
const REVEAL_SHIFT = 14;

/** Seconds between two siblings in the same group. */
const REVEAL_STAGGER = 0.08;

/**
 * Reading order, which is also the order the hero enters in. The title is
 * first on purpose and not because it sits first on the page: the first name
 * here gets a tween of its own at zero delay, and the `h1` is the LCP
 * candidate. Moving the eyebrow ahead of it would queue the `h1` behind
 * another element, which is the `1112 ms` case measured above.
 */
const HERO_ORDER = [
  "title",
  "eyebrow",
  "lede",
  "actions",
  "agents",
  "install",
  "facts",
  "badge",
] as const;

/** Seconds the sheen takes while it is making its entrance pass. */
const IGNITION_SHEEN = 1.1;

/**
 * The hero's entrance. The title is its own tween starting at zero, and the
 * rest are one staggered tween beside it: a single `stagger` across all eight
 * would put the title in a queue, and a queued title is the difference between
 * `88 ms` and `1112 ms` of LCP.
 */
function heroEntrance(hero: HTMLElement): void {
  const items = HERO_ORDER.map((name) =>
    hero.querySelector<HTMLElement>(`[data-hero-item="${name}"]`),
  ).filter((node): node is HTMLElement => node !== null);
  const [title, ...rest] = items;
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

  const field = hero.querySelector<HTMLElement>("[data-hero-field]");
  if (field === null) {
    return;
  }

  // The backdrop powers on with the copy. The sheen is a `::after` with a CSS
  // loop, which no tween can reach, so the hero declares its duration as
  // `--sheen-duration`: this borrows it for one fast pass and then removes the
  // property, which hands the element back to the stylesheet's own value
  // instead of pinning a copy of it.
  timeline
    .set(field, { "--sheen-duration": `${IGNITION_SHEEN}s` }, 0)
    .from(field, { opacity: 0, duration: 0.9, ease: "power2.out" }, 0)
    .call(
      () => field.style.removeProperty("--sheen-duration"),
      undefined,
      IGNITION_SHEEN,
    );
}

/**
 * The halo that follows the pointer across the band. `quickTo` reuses one tween
 * per axis instead of allocating a new one per event, and the element is
 * translated rather than repositioned, so the whole thing stays on the
 * compositor and never reads back into layout.
 */
function heroPointer(hero: HTMLElement): void {
  const halo = hero.querySelector<HTMLElement>("[data-hero-halo]");
  if (halo === null) {
    return;
  }

  const toX = gsap.quickTo(halo, "x", { duration: 0.5, ease: "power3.out" });
  const toY = gsap.quickTo(halo, "y", { duration: 0.5, ease: "power3.out" });

  hero.addEventListener("pointermove", (event) => {
    const box = hero.getBoundingClientRect();
    toX(event.clientX - box.left);
    toY(event.clientY - box.top);
  });
  hero.addEventListener("pointerenter", () => {
    gsap.to(halo, { opacity: 1, duration: 0.4 });
  });
  hero.addEventListener("pointerleave", () => {
    gsap.to(halo, { opacity: 0, duration: 0.4 });
  });
}

/** The two calls to action leaning toward the pointer while it is over them. */
function heroMagnetic(hero: HTMLElement): void {
  for (const target of hero.querySelectorAll<HTMLElement>(
    '[data-hero-item="actions"] a',
  )) {
    target.addEventListener("pointermove", (event) => {
      const box = target.getBoundingClientRect();
      gsap.to(target, {
        x: ((event.clientX - box.left) / box.width - 0.5) * 8,
        y: ((event.clientY - box.top) / box.height - 0.5) * 6,
        scale: 1.03,
        duration: 0.35,
        ease: "power3.out",
      });
    });
    target.addEventListener("pointerleave", () => {
      gsap.to(target, {
        x: 0,
        y: 0,
        scale: 1,
        duration: 0.45,
        ease: "power3.out",
      });
    });
  }
}

/**
 * Starts the motion layer. Reduced-motion users get the static page: the guard
 * returns before any plugin is registered, so nothing is instrumented at all
 * rather than instrumented and then skipped.
 *
 * There is no teardown: the landing is a single prerendered document with no
 * client-side router, so a ScrollTrigger lives exactly as long as its page.
 */
export function startMotion(): void {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    return;
  }

  gsap.registerPlugin(ScrollTrigger);

  // The hero first: it is the only thing on screen when the module runs, and
  // its entrance must not queue behind work done for content below the fold.
  const hero = document.querySelector<HTMLElement>("[data-hero]");
  if (hero !== null) {
    heroEntrance(hero);
    heroPointer(hero);
    heroMagnetic(hero);
  }

  for (const group of document.querySelectorAll<HTMLElement>("[data-reveal]")) {
    const items = Array.from(group.children);
    if (items.length === 0) {
      continue;
    }

    gsap.from(items, {
      opacity: 0,
      y: REVEAL_SHIFT,
      duration: 0.5,
      ease: "power2.out",
      stagger: REVEAL_STAGGER,
      scrollTrigger: {
        trigger: group,
        // Late enough that a block is committed to entering before it moves,
        // early enough that it is never already read when it does.
        start: "top 88%",
        once: true,
      },
    });
  }

  const field = document.querySelector<HTMLElement>("[data-hero-field]");
  if (field !== null) {
    // The grid drifts a fraction of the scroll distance, which is what gives
    // the band depth without moving anything a reader is looking at. `scrub`
    // ties it to the scrollbar, so it cannot run on its own after the hero has
    // left the screen.
    gsap.to(field, {
      y: 80,
      ease: "none",
      scrollTrigger: {
        trigger: field.parentElement ?? field,
        start: "top top",
        end: "bottom top",
        scrub: true,
      },
    });
  }
}
