import { useEffect, useState, type CSSProperties } from "react";
import {
  APIError,
  errorMessage,
  fetchDiagnostics,
  openLogFolder,
  qualityOptions,
  type Settings,
} from "../api";
import type { PreferencesPageProps } from "../types";
import type { ThemeMode } from "../theme";
import { copy, format } from "../copy";
import {
  checkForDesktopUpdates,
  installDesktopUpdate,
  restartDesktopApp,
  updateChecksSupported,
  type DesktopUpdateStatus,
} from "../updates";
import { SPEED_OPTIONS, cleanDisplayLabel, providerAudioModes } from "../utils";
import {
  subtitleCueStyles,
  useSubtitleCuePreferences,
  type SubtitleEdgeStyle,
} from "../subtitles";
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


function providerHealthLabel(state?: string): string {
  switch (state) {
    case "reachable":
      return copy.preferences.healthReachable;
    case "degraded":
      return copy.preferences.healthDegraded;
    case "unavailable":
      return copy.preferences.healthUnavailable;
    default:
      return copy.preferences.healthUnknown;
  }
}
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

const SUBTITLE_BACKGROUND_CHOICES: SelectOption[] = [
  { value: "0", label: copy.preferences.subtitleBackgroundTransparent },
  { value: "0.35", label: copy.preferences.subtitleBackgroundSubtle },
  { value: "0.72", label: copy.preferences.subtitleBackgroundDefault },
  { value: "0.85", label: copy.preferences.subtitleBackgroundStrong },
  { value: "1", label: copy.preferences.subtitleBackgroundSolid },
];

const SUBTITLE_EDGE_CHOICES: SelectOption[] = [
  { value: "none", label: copy.preferences.subtitleEdgeNone },
  { value: "outline", label: copy.preferences.subtitleEdgeOutline },
  { value: "shadow", label: copy.preferences.subtitleEdgeShadow },
];

export default function Preferences({
  settings,
  providers,
  healthBusy,
  healthError,
  onRefreshProviderHealth,
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
  playing = false,
}: PreferencesPageProps) {
  const [draft, setDraft] = useState<Settings>(settings);
  const [action, setAction] = useState<PendingAction>(null);
  const [initialised, setInitialised] = useState(false);
  const [diagBusy, setDiagBusy] = useState<"copy" | "open" | null>(null);
  const [diagMessage, setDiagMessage] = useState<{ tone: "status" | "error"; text: string } | null>(
    null,
  );
  // Update flow state: every transition is user-initiated (check, install,
  // restart) and the restart requires explicit consent, especially while
  // something is playing.
  const [updateStatus, setUpdateStatus] = useState<DesktopUpdateStatus | null>(null);
  const [updatePhase, setUpdatePhase] = useState<"idle" | "checking" | "downloading" | "installed">(
    "idle",
  );
  const [updateError, setUpdateError] = useState("");
  const [updateProgress, setUpdateProgress] = useState(0);
  const cuePrefs = useSubtitleCuePreferences();
  const cueStyles = subtitleCueStyles(cuePrefs.backgroundOpacity, cuePrefs.edgeStyle);

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


  // Diagnostics are strictly user-initiated: the report is fetched and
  // copied only on this deliberate click, never on render or navigation.
  async function copyDiagnostics() {
    if (diagBusy) return;
    setDiagBusy("copy");
    setDiagMessage(null);
    try {
      const report = await fetchDiagnostics();
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(JSON.stringify(report, null, 2));
      setDiagMessage({ tone: "status", text: copy.preferences.diagnosticsCopied });
    } catch {
      setDiagMessage({ tone: "error", text: copy.preferences.diagnosticsCopyError });
    } finally {
      setDiagBusy(null);
    }
  }

  async function openFolder() {
    if (diagBusy) return;
    setDiagBusy("open");
    setDiagMessage(null);
    try {
      await openLogFolder();
    } catch (reason) {
      setDiagMessage({
        tone: "error",
        text:
          reason instanceof APIError && reason.code === "NOT_SUPPORTED"
            ? copy.preferences.logFolderUnsupported
            : errorMessage(reason, copy.preferences.logFolderError),
      });
    } finally {
      setDiagBusy(null);
    }
  }

  // Manual update check: never automatic, never on page load.
  async function checkUpdates() {
    if (updatePhase === "checking" || updatePhase === "downloading") return;
    setUpdatePhase("checking");
    setUpdateError("");
    try {
      setUpdateStatus(await checkForDesktopUpdates());
    } catch {
      setUpdateStatus(null);
      setUpdateError(copy.states.requestFailed);
    } finally {
      setUpdatePhase("idle");
    }
  }

  async function installUpdate() {
    if (updatePhase === "downloading" || updateStatus?.state !== "available") return;
    setUpdatePhase("downloading");
    setUpdateError("");
    setUpdateProgress(0);
    try {
      await installDesktopUpdate((progress) => {
        setUpdateProgress(
          progress.total > 0 ? Math.min(100, Math.round((progress.downloaded / progress.total) * 100)) : 0,
        );
      });
      setUpdatePhase("installed");
    } catch {
      setUpdatePhase("idle");
      setUpdateError(copy.preferences.downloadError);
    }
  }

  async function restartApp() {
    if (updatePhase !== "installed") return;
    try {
      await restartDesktopApp();
    } catch {
      setUpdateError(copy.states.requestFailed);
    }
  }
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
              title={copy.preferences.subtitleBackground}
              description={copy.preferences.subtitleAppearanceDescription}
              htmlFor="prefs-subtitle-background"
            >
              <select
                id="prefs-subtitle-background"
                className="prefs-select"
                value={SUBTITLE_BACKGROUND_CHOICES.some((option) => Number(option.value) === cuePrefs.backgroundOpacity) ? String(cuePrefs.backgroundOpacity) : "0.72"}
                onChange={(event) => cuePrefs.setBackgroundOpacity(Number(event.target.value))}
              >
                {SUBTITLE_BACKGROUND_CHOICES.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </SettingRow>
            <SettingRow
              title={copy.preferences.subtitleEdgeStyle}
              description={copy.preferences.subtitleAppearanceDescription}
              htmlFor="prefs-subtitle-edge"
            >
              <select
                id="prefs-subtitle-edge"
                className="prefs-select"
                value={cuePrefs.edgeStyle}
                onChange={(event) => cuePrefs.setEdgeStyle(event.target.value as SubtitleEdgeStyle)}
              >
                {SUBTITLE_EDGE_CHOICES.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </SettingRow>
            <div className="setting-row prefs-subtitle-row">
              <div className="setting-row__text">
                <p className="setting-row__title">{copy.preferences.subtitlePreview}</p>
              </div>
              <div className="setting-row__control prefs-subtitle-preview" aria-hidden="true">
                <span style={cueStyles as CSSProperties}>{copy.preferences.subtitlePreviewText}</span>
              </div>
            </div>
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
            <SettingRow
              title={copy.preferences.providerHealth}
              description={copy.preferences.providerHealthDescription}
            >
              <Button
                variant="secondary"
                busy={healthBusy}
                disabled={providers.length === 0}
                onClick={onRefreshProviderHealth}
              >
                {healthBusy ? copy.preferences.healthChecking : copy.preferences.healthRefresh}
              </Button>
            </SettingRow>
            {healthError ? <Notice tone="error" message={healthError} /> : null}
            {providers.length > 0 ? (
              <ul className="prefs-providers">
                {providers.map((item) => (
                  <li className="prefs-providers__item" key={item.id}>
                    <span className="prefs-providers__name">{cleanDisplayLabel(item.id)}</span>
                    <span className="prefs-providers__modes">
                      {providerAudioModes(item).map((mode) => (
                        <Badge key={mode} tone="neutral">
                          {mode === "dub" ? copy.preferences.dub : copy.preferences.sub}
                        </Badge>
                      ))}
                    </span>
                    <Badge tone={item.enabled ? "accent" : "neutral"}>
                      {item.enabled ? copy.preferences.enabled : copy.preferences.disabled}
                    </Badge>
                    <span className="prefs-providers__health">
                      <Badge tone={item.health?.state === "reachable" ? "accent" : "neutral"}>
                        {providerHealthLabel(item.health?.state)}
                      </Badge>
                      {item.health?.checkedAt ? (
                        <span className="prefs-providers__checked">
                          {copy.preferences.healthCheckedAt.replace(
                            "{time}",
                            new Date(item.health.checkedAt).toLocaleString(),
                          )}
                        </span>
                      ) : null}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="prefs-empty">{copy.preferences.noProviders}</p>
            )}
          </section>

          <section className="prefs-section" aria-labelledby="prefs-diagnostics">
            <SectionHeader id="prefs-diagnostics" title={copy.preferences.diagnostics} />
            <p className="prefs-section__description">{copy.preferences.diagnosticsDescription}</p>
            <SettingRow
              title={copy.preferences.copyDiagnostics}
              description={copy.preferences.diagnosticsDescription}
            >
              <Button
                variant="secondary"
                busy={diagBusy === "copy"}
                disabled={diagBusy !== null}
                onClick={() => void copyDiagnostics()}
              >
                {diagBusy === "copy"
                  ? copy.preferences.copyingDiagnostics
                  : copy.preferences.copyDiagnostics}
              </Button>
            </SettingRow>
            <SettingRow
              title={copy.preferences.openLogFolder}
              description={copy.preferences.openLogFolderDescription}
            >
              <Button
                variant="secondary"
                busy={diagBusy === "open"}
                disabled={diagBusy !== null}
                onClick={() => void openFolder()}
              >
                {diagBusy === "open"
                  ? copy.preferences.openingLogFolder
                  : copy.preferences.openLogFolder}
              </Button>
            </SettingRow>
            {diagMessage ? <Notice tone={diagMessage.tone} message={diagMessage.text} /> : null}
          </section>

          <section className="prefs-section" aria-labelledby="prefs-updates">
            <SectionHeader id="prefs-updates" title={copy.preferences.updates} />
            <p className="prefs-section__description">{copy.preferences.updatesDescription}</p>
            {updateChecksSupported() ? (
              <>
                <SettingRow
                  title={copy.preferences.checkUpdates}
                  description={copy.preferences.updatesDescription}
                >
                  <Button
                    variant="secondary"
                    busy={updatePhase === "checking"}
                    disabled={updatePhase === "checking" || updatePhase === "downloading"}
                    onClick={() => void checkUpdates()}
                  >
                    {updatePhase === "checking"
                      ? copy.preferences.checkingUpdates
                      : copy.preferences.checkUpdates}
                  </Button>
                </SettingRow>
                {updateStatus && updateStatus.state === "up-to-date" ? (
                  <Notice tone="status" message={copy.preferences.upToDate} />
                ) : null}
                {updateStatus && updateStatus.state === "unavailable" ? (
                  <Notice tone="status" message={copy.preferences.updateUnavailable} />
                ) : null}
                {updateStatus && updateStatus.state === "available" && updatePhase !== "installed" ? (
                  <div className="prefs-section__description">
                    <p>{format(copy.preferences.updateFound, { version: updateStatus.version ?? "" })}</p>
                    {updateStatus.notes ? (
                      <details>
                        <summary>{copy.preferences.updateNotesLabel}</summary>
                        <p>{updateStatus.notes}</p>
                      </details>
                    ) : null}
                    <Button
                      variant="secondary"
                      busy={updatePhase === "downloading"}
                      disabled={updatePhase === "downloading" || updatePhase === "checking"}
                      onClick={() => void installUpdate()}
                    >
                      {updatePhase === "downloading"
                        ? updateProgress > 0
                          ? format(copy.preferences.downloadProgress, { percent: updateProgress })
                          : copy.preferences.downloadPrepare
                        : copy.preferences.downloadInstall}
                    </Button>
                  </div>
                ) : null}
                {updatePhase === "installed" ? (
                  <div className="prefs-section__description">
                    <p>
                      {format(copy.preferences.updateInstalled, {
                        version: updateStatus?.version ?? "",
                      })}
                    </p>
                    <p>{copy.preferences.restartConsent}</p>
                    {playing ? <p>{copy.preferences.restartPlaying}</p> : null}
                    <div className="prefs-actions">
                      <Button variant="primary" onClick={() => void restartApp()}>
                        {copy.preferences.restartNow}
                      </Button>
                      <Button variant="secondary" onClick={() => setUpdatePhase("idle")}>
                        {copy.preferences.restartLater}
                      </Button>
                    </div>
                  </div>
                ) : null}
                {updateError ? <Notice tone="error" message={updateError} /> : null}
              </>
            ) : (
              <p className="prefs-empty">{copy.preferences.updatesUnsupported}</p>
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
