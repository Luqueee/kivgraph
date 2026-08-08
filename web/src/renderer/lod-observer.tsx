import { useFrame, useThree } from "@react-three/fiber";
import { useEffect, useRef } from "react";

import { lodKindForCamera, type ViewerLodKind } from "./lod";

export interface CameraLodObserverProps {
  readonly center: readonly [number, number, number];
  readonly previous: ViewerLodKind;
  readonly onChange: (kind: ViewerLodKind) => void;
}

/**
 * Samples the camera only while R3F is rendering an interaction frame.
 * Reagraph's controls update the camera inside that loop, so this remains
 * independent of camera-controls event timing and does no work at rest.
 */
export function CameraLodObserver({
  center,
  previous,
  onChange,
}: CameraLodObserverProps): null {
  const camera = useThree((state) => state.camera);
  const viewportHeight = useThree((state) => state.size.height);
  const last = useRef(previous);

  useEffect(() => {
    last.current = previous;
  }, [previous]);

  useFrame(() => {
    if (!("isPerspectiveCamera" in camera)) return;
    const dx = camera.position.x - center[0];
    const dy = camera.position.y - center[1];
    const dz = camera.position.z - center[2];
    const distance = Math.hypot(dx, dy, dz);
    const next = lodKindForCamera(
      {
        distance,
        fov: camera.fov,
        viewportHeight,
      },
      last.current,
    );
    if (next === last.current) return;
    last.current = next;
    onChange(next);
  });

  return null;
}
