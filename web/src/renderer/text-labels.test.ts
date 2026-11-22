import { describe, expect, it } from "vitest";
import { captionPlacement, setTextLabelsHidden } from "@/renderer/text-labels";

function scene(objects: unknown[]) {
  return {
    traverse: (visit: (object: never) => void) => {
      for (const object of objects) visit(object as never);
    },
  };
}

describe("text label visibility", () => {
  it("only touches Troika text meshes", () => {
    const label = { visible: true, material: { isTroikaTextMaterial: true } };
    const node = { visible: true, material: { isTroikaTextMaterial: false } };
    const group = { visible: true };

    const changed = setTextLabelsHidden(scene([label, node, group]), true);

    expect(changed).toBe(1);
    expect(label.visible).toBe(false);
    expect(node.visible).toBe(true);
    expect(group.visible).toBe(true);
  });

  it("finds a text mesh behind a material array", () => {
    const label = {
      visible: true,
      material: [
        { isTroikaTextMaterial: false },
        { isTroikaTextMaterial: true },
      ],
    };

    expect(setTextLabelsHidden(scene([label]), true)).toBe(1);
    expect(label.visible).toBe(false);
  });

  it("reports no change when the labels already match, and restores them", () => {
    const label = { visible: false, material: { isTroikaTextMaterial: true } };

    expect(setTextLabelsHidden(scene([label]), true)).toBe(0);
    expect(setTextLabelsHidden(scene([label]), false)).toBe(1);
    expect(label.visible).toBe(true);
  });
});

describe("sprite captions", () => {
  it("are hidden with the Troika ones while the camera moves", () => {
    const sprite = { visible: true, material: { userData: { caption: true } } };
    const node = { visible: true, material: { userData: {} } };
    expect(setTextLabelsHidden(scene([sprite, node]), true)).toBe(1);
    expect(sprite.visible).toBe(false);
    expect(node.visible).toBe(true);
    expect(setTextLabelsHidden(scene([sprite, node]), false)).toBe(1);
    expect(sprite.visible).toBe(true);
  });

  it("sit below the node, as tall as a label and as wide as their text", () => {
    const placed = captionPlacement(22, 256, 64);
    expect(placed.position[1]).toBeLessThan(-22);
    expect(placed.scale[1]).toBeCloseTo(8.4);
    expect(placed.scale[0]).toBeCloseTo(8.4 * 4);
    // A texture with no height is not a division by zero.
    expect(captionPlacement(4, 10, 0).scale[0]).toBeCloseTo(8.4);
  });
});
