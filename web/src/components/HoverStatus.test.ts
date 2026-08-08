import { describe, expect, it } from "vitest";
import { createStatusChannel } from "@/components/HoverStatus";

describe("hover status channel", () => {
  it("publishes a new caption to every subscriber", () => {
    const channel = createStatusChannel("idle");
    const seen: string[] = [];
    const unsubscribe = channel.subscribe(() => seen.push(channel.get()));

    channel.set("node · repositories");
    channel.set("other · packages");
    unsubscribe();
    channel.set("after");

    expect(seen).toEqual(["node · repositories", "other · packages"]);
    expect(channel.get()).toBe("after");
  });

  it("stays silent when the caption does not change", () => {
    const channel = createStatusChannel("idle");
    let notified = 0;
    channel.subscribe(() => {
      notified += 1;
    });

    channel.set("idle");
    channel.set("hover");
    channel.set("hover");

    expect(notified).toBe(1);
  });
});
