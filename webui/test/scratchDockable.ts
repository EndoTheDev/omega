// omega webui: the TEST-ONLY scratch singleton. The singleton-opener / tab-ops contract
// still holds for "any main singleton", so the dock tests mint this synthetic entry
// into the registry, exercise the paths, and always restore in `finally`.
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
    icon: "module",
    policy: "singleton",
    class: "main",
    area: "main",
    closable: true,
  } as unknown as never);
  (DOCK_COMPONENTS as unknown as string[]).push(SCRATCH_ID);
  return () => {
    (DOCKABLES as unknown as unknown[]).pop();
    (DOCK_COMPONENTS as unknown as string[]).pop();
  };
}

/** runs `fn` with a synthetic MAIN-area singleton ("scratch") registered in the. */
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