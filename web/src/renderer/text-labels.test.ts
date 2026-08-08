import { describe, expect, it } from "vitest";
import { setTextLabelsHidden } from "@/renderer/text-labels";

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
