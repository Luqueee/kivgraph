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
