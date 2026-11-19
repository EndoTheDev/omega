import { ref, type Ref } from "vue";

const pending = ref<string | null>(null);

/** ask the settings pane to jump to `section` (a category id) */
export function jumpToSettingsSection(section: string): void {
  pending.value = section;
}

/** the pane's read side: the pending jump, cleared by the consumer */
export function useSettingsJump(): Ref<string | null> {
  return pending;
}
