import { useEffect, useState } from "react";
import { qualityOptions, type Settings } from "../api";
import type { PreferencesPageProps } from "../types";
import type { ThemeMode } from "../theme";
import { copy } from "../copy";
import { SPEED_OPTIONS, cleanDisplayLabel } from "../utils";
import {
  Badge,
  Button,
  Notice,
  PageHeader,
  SectionHeader,
  SettingRow,
  Spinner,
} from "../components";
import "../styles/pages-library.css";

interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

const LANGUAGE_OPTIONS: SelectOption[] = [
  { value: "id", label: copy.preferences.languageIndonesian },
  { value: "en", label: copy.preferences.languageEnglish },
  { value: "ja", label: copy.preferences.languageJapanese },
];

type PendingAction = "save" | "reset" | "reload" | null;

function speedLabel(value: number) {
  return `${value}×`;
}

export default function Preferences({
  settings,
  providers,
  theme,
  sessionSpeed,
  busy,
  loading,
  saveError,
  onTheme,
  onSave,
  onReset,
  onReload,
  onSpeedChange,
  onClearHistory,
  onClearList,
  onClearCache,
}: PreferencesPageProps) {
  const [draft, setDraft] = useState<Settings>(settings);
  const [action, setAction] = useState<PendingAction>(null);
  const [initialised, setInitialised] = useState(false);

  useEffect(() => {
    setDraft(settings);
  }, [settings]);

  useEffect(() => {
    if (!busy) setAction((current) => (current === "save" || current === "reset" ? null : current));
  }, [busy]);

  useEffect(() => {
    if (!loading) {
      setInitialised(true);
      setAction((current) => (current === "reload" ? null : current));
    }
  }, [loading]);

  function patch(next: Partial<Settings>) {
    setDraft((current) => ({ ...current, ...next }));
  }

  function save() {
    if (busy) return;
    setAction("save");
    onSave({
      subtitleLanguage: draft.subtitleLanguage,
      quality: draft.quality,
      autoplay: draft.autoplay,
      preferredProvider: draft.preferredProvider,
      revision: settings.revision,
    });
  }

  function reset() {
    if (busy) return;
    setAction("reset");
    onReset();
  }

  function reload() {
    if (busy) return;
    setAction("reload");
    onReload();
  }

  const formPending = busy || loading;
  const saveBusy = busy && action === "save";
  const resetBusy = busy && action === "reset";
  const reloadBusy = action === "reload" && (busy || loading);
  const statusText = saveBusy
    ? copy.preferences.saving
    : reloadBusy
      ? copy.preferences.loading
      : resetBusy
        ? copy.states.busy
        : "";

  const subtitleOptions: SelectOption[] = [...LANGUAGE_OPTIONS];
  const currentLanguage = draft.subtitleLanguage?.trim();
  if (currentLanguage && !subtitleOptions.some((option) => option.value === currentLanguage)) {
    subtitleOptions.push({ value: currentLanguage, label: cleanDisplayLabel(currentLanguage) });
  }

  const providerOptions: SelectOption[] = [
    { value: "auto", label: copy.preferences.providerAuto },
    ...providers.map((item) => ({
      value: item.id,
      label: cleanDisplayLabel(item.id),
      disabled: !item.enabled && item.id !== draft.preferredProvider,
    })),
  ];
  if (draft.preferredProvider && !providerOptions.some((option) => option.value === draft.preferredProvider)) {
    providerOptions.push({
      value: draft.preferredProvider,
      label: cleanDisplayLabel(draft.preferredProvider),
    });
  }

  const qualityLabel = (value: string) => (value === "auto" ? copy.preferences.qualityAuto : value);

  if (loading && !initialised) {
    return (
      <div className="page prefs-page">
        <div className="container prefs-container">
          <PageHeader title={copy.preferences.title} description={copy.preferences.description} />
          <div className="prefs-loading" aria-busy="true">
            <Spinner label={copy.preferences.loading} />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page prefs-page">
      <div className="container prefs-container">
        <PageHeader title={copy.preferences.title} description={copy.preferences.description} />

        <form
          className="prefs-form"
          onSubmit={(event) => {
            event.preventDefault();
            save();
          }}
        >
          <section className="prefs-section" aria-labelledby="prefs-appearance">
            <SectionHeader id="prefs-appearance" title={copy.preferences.appearance} />
            <SettingRow
              title={copy.preferences.theme}
              description={copy.preferences.themeDescription}
              htmlFor="prefs-theme"
            >
              <select
                id="prefs-theme"
                className="prefs-select"
                value={theme}
                disabled={busy}
                onChange={(event) => onTheme(event.target.value as ThemeMode)}
              >
                <option value="system">{copy.preferences.system}</option>
                <option value="light">{copy.preferences.light}</option>
                <option value="dark">{copy.preferences.dark}</option>
              </select>
            </SettingRow>
          </section>

          <section className="prefs-section" aria-labelledby="prefs-playback">
            <SectionHeader id="prefs-playback" title={copy.preferences.playback} />
            <SettingRow
              title={copy.preferences.quality}
              description={
                draft.quality === "auto"
                  ? copy.preferences.qualityDescriptionAuto
                  : copy.preferences.qualityDescription
              }
              htmlFor="prefs-quality"
            >
              <select
                id="prefs-quality"
                className="prefs-select"
                value={draft.quality}
                disabled={formPending}
                onChange={(event) => patch({ quality: event.target.value as Settings["quality"] })}
              >
                {qualityOptions.map((value) => (
                  <option key={value} value={value}>
                    {qualityLabel(value)}
                  </option>
                ))}
              </select>
            </SettingRow>
            <SettingRow
              title={copy.preferences.speed}
              description={copy.preferences.speedDescription}
              htmlFor="prefs-speed"
            >
              <select
                id="prefs-speed"
                className="prefs-select"
                value={String(sessionSpeed)}
                onChange={(event) => onSpeedChange(Number(event.target.value))}
              >
                {SPEED_OPTIONS.map((value) => (
                  <option key={value} value={String(value)}>
                    {speedLabel(value)}
                  </option>
                ))}
              </select>
            </SettingRow>
            <SettingRow
              title={copy.preferences.subtitleLanguage}
              description={copy.preferences.subtitleLanguageDescription}
              htmlFor="prefs-subtitles"
            >
              <select
                id="prefs-subtitles"
                className="prefs-select"
                value={draft.subtitleLanguage}
                disabled={formPending}
                onChange={(event) => patch({ subtitleLanguage: event.target.value })}
              >
                {subtitleOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </SettingRow>
            <SettingRow
              title={copy.preferences.provider}
              description={copy.preferences.providerDescription}
              htmlFor="prefs-provider"
            >
              <select
                id="prefs-provider"
                className="prefs-select"
                value={draft.preferredProvider}
                disabled={formPending}
                onChange={(event) => patch({ preferredProvider: event.target.value })}
              >
                {providerOptions.map((option) => (
                  <option key={option.value} value={option.value} disabled={option.disabled}>
                    {option.label}
                  </option>
                ))}
              </select>
            </SettingRow>
            <SettingRow
              title={copy.preferences.autoplay}
              description={copy.preferences.autoplayDescription}
              htmlFor="prefs-autoplay"
            >
              <span className="switch">
                <input
                  id="prefs-autoplay"
                  className="switch__input"
                  type="checkbox"
                  checked={draft.autoplay}
                  disabled={formPending}
                  onChange={(event) => patch({ autoplay: event.target.checked })}
                />
                <span className="switch__track" aria-hidden="true" />
              </span>
            </SettingRow>
          </section>

          <section className="prefs-section" aria-labelledby="prefs-data">
            <SectionHeader id="prefs-data" title={copy.preferences.data} />
            <p className="prefs-section__description">{copy.preferences.dataDescription}</p>
            <SettingRow title={copy.preferences.history} description={copy.preferences.historyDescription}>
              <Button variant="danger" disabled={formPending} onClick={onClearHistory}>
                {copy.preferences.clearHistory}
              </Button>
            </SettingRow>
            <SettingRow title={copy.preferences.list} description={copy.preferences.listDescription}>
              <Button variant="danger" disabled={formPending} onClick={onClearList}>
                {copy.preferences.clearList}
              </Button>
            </SettingRow>
            <SettingRow title={copy.preferences.cache} description={copy.preferences.cacheDescription}>
              <Button variant="danger" disabled={formPending} onClick={onClearCache}>
                {copy.preferences.clearCache}
              </Button>
            </SettingRow>
          </section>

          <section className="prefs-section" aria-labelledby="prefs-availability">
            <SectionHeader id="prefs-availability" title={copy.preferences.providerStatus} />
            {providers.length > 0 ? (
              <ul className="prefs-providers">
                {providers.map((item) => (
                  <li className="prefs-providers__item" key={item.id}>
                    <span className="prefs-providers__name">{cleanDisplayLabel(item.id)}</span>
                    <Badge tone={item.enabled ? "accent" : "neutral"}>
                      {item.enabled ? copy.preferences.enabled : copy.preferences.disabled}
                    </Badge>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="prefs-empty">{copy.preferences.noProviders}</p>
            )}
          </section>

          <div className="prefs-footer">
            {saveError ? <Notice tone="error" message={saveError} /> : null}
            <p className="visually-hidden" role="status">
              {statusText}
            </p>
            <div className="prefs-actions">
              <Button type="submit" variant="primary" busy={saveBusy} disabled={formPending}>
                {copy.preferences.save}
              </Button>
              <Button variant="secondary" busy={resetBusy} disabled={formPending} onClick={reset}>
                {copy.preferences.reset}
              </Button>
              <Button variant="secondary" busy={reloadBusy} disabled={formPending} onClick={reload}>
                {copy.preferences.reload}
              </Button>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
