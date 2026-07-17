import { LocalStorage, type QVueGlobals } from "quasar"

export type ThemePreference = "dark" | "light" | "system"

export const THEME_PREFERENCE_KEY = "radioledger.ui_theme"

export function isThemePreference(value: unknown): value is ThemePreference {
  return value === "dark" || value === "light" || value === "system"
}

export function getStoredThemePreference(): ThemePreference | null {
  const rawValue = LocalStorage.getItem(THEME_PREFERENCE_KEY)
  return isThemePreference(rawValue) ? rawValue : null
}

export function applyThemePreference($q: QVueGlobals, theme: ThemePreference): void {
  LocalStorage.set(THEME_PREFERENCE_KEY, theme)
  $q.dark.set(theme === "system" ? "auto" : theme === "dark")
}
