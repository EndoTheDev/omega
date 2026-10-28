// omega webui: the TEST-ONLY scratch singleton. card t_49d0b308 moved
// settings into the inspector (pinned), so no REAL component lives in
// the main-singleton class anymore - chat is multi and everything else
// is pinned. The singleton-opener / tab-ops contract still holds for
// "any main singleton", so the engine tests mint this synthetic entry
// into the registry, exercise the paths, and always restore in
// `finally`. Registered at the array ends; tests run sequentially, so
// nothing else observes it.
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  type DockComponentName,
} from "../app/composables/dockables.ts";

export const SCRATCH_ID = "scratch";

/** registers the scratch singleton until the returned function runs. */
export function registerScratchSingleton(): () => void {
  DOCKABLES.push({
    id: SCRATCH_ID,
    title: "scratch",
    // card t_502b8e81: the registry icon is an iconset NAME now
    icon: "module",
    policy: "singleton",
    area: "main",
    closable: true,
  } as unknown as never);
  (DOCK_COMPONENTS as unknown as string[]).push(SCRATCH_ID);
  return () => {
    (DOCKABLES as unknown as unknown[]).pop();
    (DOCK_COMPONENTS as unknown as string[]).pop();
  };
}

/** runs `fn` with a synthetic MAIN-area singleton ("scratch") registered
    in the dockable registry (openable, closable, main-area). The id is
    passed as the component id; casts stay inside here. */
export async function withScratchSingleton(
  fn: (id: DockComponentName) => void | Promise<void>,
): Promise<void> {
  const restore = registerScratchSingleton();
  try {
    await fn(SCRATCH_ID as DockComponentName);
  } finally {
    restore();
  }
}