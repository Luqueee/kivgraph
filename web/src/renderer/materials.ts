import { extend } from "@react-three/fiber";
import {
  LineBasicMaterial,
  MeshBasicMaterial,
  MeshPhongMaterial,
  SpriteMaterial,
} from "three";
import {
  LineBasicNodeMaterial,
  MeshBasicNodeMaterial,
  MeshPhongNodeMaterial,
  SpriteNodeMaterial,
} from "three/webgpu";

import type { RendererBackend } from "./webgpu";
/**
 * Reagraph creates these elements by their ordinary Three names. WebGPU's
 * renderer accepts node materials rather than classic mesh and sprite
 * materials, so the same JSX can remain shared by both backends.
 */
export function configureViewerMaterials(backend: RendererBackend): void {
  if (backend === "webgpu") {
    extend({
      LineBasicMaterial: LineBasicNodeMaterial,
      MeshBasicMaterial: MeshBasicNodeMaterial,
      MeshPhongMaterial: MeshPhongNodeMaterial,
      SpriteMaterial: SpriteNodeMaterial,
    });
    return;
  }
  extend({
    LineBasicMaterial,
    MeshBasicMaterial,
    MeshPhongMaterial,
    SpriteMaterial,
  });
}
