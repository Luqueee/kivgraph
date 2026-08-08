import { useCallback, useSyncExternalStore } from "react";

export interface StatusChannel {
  get(): string;
  set(text: string): void;
  subscribe(listener: () => void): () => void;
}

/**
 * A one-line readout that lives outside React state.
 *
 * The pointer crosses a dozen nodes on the way to the one it wants, and every
 * caption written into the viewer's state re-renders the graph canvas: React
 * Three Fiber recreates the element tree for every node Reagraph draws. The
 * channel keeps that text out of the viewer so only the caption re-renders.
 */
export function createStatusChannel(initial: string): StatusChannel {
  let text = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => text,
    set: (next) => {
      if (next === text) return;
      text = next;
      for (const listener of listeners) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

export function HoverStatus({
  channel,
}: {
  readonly channel: StatusChannel;
}): React.ReactElement {
  const subscribe = useCallback(
    (listener: () => void) => channel.subscribe(listener),
    [channel],
  );
  const text = useSyncExternalStore(subscribe, channel.get, channel.get);
  return <>{text}</>;
}
