import { useEffect, useMemo, useRef, useState } from "react";
import type {
  ChangeEvent,
  CSSProperties,
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
} from "react";
import Hls from "hls.js";
import {
  ArrowLeft,
  Captions,
  Check,
  ChevronLeft,
  ChevronRight,
  ListVideo,
  Maximize,
  Minimize,
  MonitorPlay,
  Pause,
  Play,
  RotateCcw,
  Settings as SettingsIcon,
  SkipForward,
  Upload,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import {
  APIError,
  api,
  errorMessage,
  id,
  json,
  mediaURL,
  type Anime,
  type Episode,
  type PlaybackSession,
  type Progress,
  type Provider,
  type Settings,
  type Subtitle,
} from "./api";
import { copy, format } from "./copy";
import { SPEED_OPTIONS, clamp, cleanDisplayLabel, cleanEpisodeTitle, formatClock, percent, uniqueLabels } from "./utils";
import { SUBTITLE_SCALE_MAX, SUBTITLE_SCALE_MIN, useSubtitleScale } from "./subtitles";
import "./styles/player.css";

export interface PlayerProps {
  anime: Anime;
  episode: Episode;
  episodes: Episode[];
  settings: Settings;
  provider: string;
  mode: string;
  providers: Provider[];
  startPlaying?: boolean;
  sessionSpeed: number;
  onSpeedChange: (speed: number) => void;
  onProviderChange: (provider: string) => void;
  onEpisode: (episode: Episode, autoplay?: boolean) => void;
  onClose: () => void;
}

interface PendingProgress {
  positionSeconds: number;
  durationSeconds: number | null;
  ended: boolean;
  provider: string;
}

interface AudioTrackInfo {
  id: number;
  name: string;
  lang?: string;
}

interface LevelInfo {
  height: number;
  bitrate: number;
}

type PlayerStatus = "loading" | "preparing" | "ready" | "recovering" | "error";
type PlayerMenu = "settings" | "captions" | "episodes" | null;

const IDLE_DELAY_MS = 3000;
const SEEK_STEP_SECONDS = 5;
const VOLUME_STEP = 0.05;
const SUBTITLE_MAX_BYTES = 2 * 1024 * 1024;

/* Named steps keep the control honest: each value multiplies the cue base size
   the engine derives from the player surface. */
const SUBTITLE_SIZE_CHOICES: { value: number; label: string }[] = [
  { value: SUBTITLE_SCALE_MIN, label: copy.player.subtitleSizeSmall },
  { value: 1, label: copy.player.subtitleSizeDefault },
  { value: 1.3, label: copy.player.subtitleSizeLarge },
  { value: 1.65, label: copy.player.subtitleSizeHuge },
  { value: SUBTITLE_SCALE_MAX, label: copy.player.subtitleSizeMax },
];
/* Browsers fire `stalled`/`waiting` on brief network pauses even when playback
   keeps going. Wait this long before believing the clock actually stopped. */
const BUFFERING_GRACE_MS = 350;

/* Keeps background tab order intact when the player remounts for a new episode:
   focus only returns to the opener once the final player instance unmounts. */
let activePlayerCount = 0;
let returnFocusTarget: HTMLElement | null = null;

function isInteractiveTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return Boolean(
    target.closest("button, input, select, textarea, a[href], [contenteditable='true'], [role='menuitem'], [role='option']"),
  );
}

function audioTrackLabel(track: AudioTrackInfo): string {
  const name = cleanDisplayLabel(track.name?.trim() || "");
  if (name && track.lang) return name + " · " + track.lang;
  if (name) return name;
  if (track.lang) return track.lang;
  return copy.player.languageUnknown;
}

export function Player({
  anime,
  episode,
  episodes,
  settings,
  provider,
  mode,
  providers,
  startPlaying = false,
  sessionSpeed,
  onSpeedChange,
  onProviderChange,
  onEpisode,
  onClose,
}: PlayerProps) {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const rootRef = useRef<HTMLElement | null>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const sessionRef = useRef<PlaybackSession | null>(null);
  const progressRef = useRef<Progress | null>(null);
  const pendingRef = useRef<PendingProgress | null>(null);
  const savingRef = useRef(false);
  const recoveriesRef = useRef(0);
  const resumeRef = useRef(0);
  const generationRef = useRef(0);
  const lastPositionRef = useRef(0);
  const lastDurationRef = useRef<number | null>(null);
  const conflictRef = useRef(false);
  const playIntentRef = useRef(Boolean(startPlaying));
  const speedRef = useRef(sessionSpeed);
  const idleTimerRef = useRef<number | null>(null);
  const lastRevealRef = useRef(0);
  const menuRef = useRef<PlayerMenu>(null);
  const bufferingRef = useRef(false);
  const bufferingTimerRef = useRef<number | null>(null);
  const bufferingMarkRef = useRef(0);
  const seekingRef = useRef(false);
  const settingsTriggerRef = useRef<HTMLButtonElement | null>(null);
  const captionsTriggerRef = useRef<HTMLButtonElement | null>(null);
  const bottomBarRef = useRef<HTMLDivElement | null>(null);
  const episodesTriggerRef = useRef<HTMLButtonElement | null>(null);
  const settingsPanelRef = useRef<HTMLDivElement | null>(null);
  const captionsPanelRef = useRef<HTMLDivElement | null>(null);
  const episodesPanelRef = useRef<HTMLElement | null>(null);
  const focusOnOpenRef = useRef<string | null>(null);
  const revealOnlyRef = useRef(false);

  const [session, setSession] = useState<PlaybackSession | null>(null);
  const [status, setStatus] = useState<PlayerStatus>("loading");
  const [error, setError] = useState("");
  const [failureKind, setFailureKind] = useState<"media" | "provider" | "exhausted">("media");
  const [notice, setNotice] = useState("");
  const [retry, setRetry] = useState(0);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState<number | null>(null);
  const [bufferedRanges, setBufferedRanges] = useState<{ start: number; end: number }[]>([]);
  const [playing, setPlaying] = useState(false);
  const [buffering, setBuffering] = useState(false);
  const [seeking, setSeeking] = useState(false);
  const [ended, setEnded] = useState(false);
  const [levels, setLevels] = useState<LevelInfo[]>([]);
  const [audio, setAudio] = useState<AudioTrackInfo[]>([]);
  const [audioTrack, setAudioTrack] = useState(-1);
  const [quality, setQuality] = useState(-1);
  const [subtitle, setSubtitle] = useState("off");
  const [tracks, setTracks] = useState<Subtitle[]>([]);
  const [autoplay, setAutoplay] = useState(Boolean(settings.autoplay));
  const [volume, setVolume] = useState(1);
  const [muted, setMuted] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [menu, setMenu] = useState<PlayerMenu>(null);
  const [idle, setIdle] = useState(false);
  const [subtitleScale, setSubtitleScale] = useSubtitleScale();
  const [overlayMounted, setOverlayMounted] = useState(false);
  const [nativeFallback, setNativeFallback] = useState(false);
  const [announcement, setAnnouncement] = useState("");

  // Upstream track labels can be identical after provenance is stripped; keep
  // each menu entry distinguishable without exposing where it came from.
  const captionLabels = useMemo(() => {
    const cleaned = tracks.map((track) => cleanDisplayLabel(track.label));
    const unique = uniqueLabels(cleaned);
    return new Map(tracks.map((track, index) => [track.id, unique[index] ?? cleaned[index]]));
  }, [tracks]);

  menuRef.current = menu;
  bufferingRef.current = buffering;
  seekingRef.current = seeking;
  speedRef.current = sessionSpeed;

  const index = episodes.findIndex((item) => item.id === episode.id);
  const previous = index > 0 ? episodes[index - 1] : undefined;
  const next = index >= 0 && index < episodes.length - 1 ? episodes[index + 1] : undefined;
  const previousPlayable = Boolean(previous?.availability?.length);
  const nextPlayable = Boolean(next?.availability?.length);
  const showCustomOverlay = overlayMounted && !nativeFallback;

  // A movie is one work, so episode-flavoured labels ("Back to episodes") would
  // misdescribe what the viewer is watching.
  const isMovie = (anime.type ?? "").toLowerCase() === "movie";

  const durationFinite = duration !== null && Number.isFinite(duration) && duration > 0;
  const playedPercent = percent(time, duration);
  const volumePercent = percent(muted ? 0 : volume, 1);
  const seekMax = durationFinite ? (duration as number) : 1;
  const seekValue = durationFinite ? Math.min(time, duration as number) : 0;

  const save = async (endedSave = false): Promise<void> => {
    const element = videoRef.current;
    if (!sessionRef.current || conflictRef.current) return;
    pendingRef.current = {
      positionSeconds: element?.currentTime ?? lastPositionRef.current,
      durationSeconds: element && Number.isFinite(element.duration) ? element.duration : lastDurationRef.current,
      ended: endedSave,
      provider: sessionRef.current.actualProvider,
    };
    if (savingRef.current) return;
    savingRef.current = true;
    try {
      while (pendingRef.current && !conflictRef.current) {
        const snapshot = pendingRef.current;
        pendingRef.current = null;
        progressRef.current = await api<Progress>(`/progress/${id(episode.id)}`, {
          method: "PUT",
          keepalive: true,
          body: json({ ...snapshot, mode, revision: progressRef.current?.revision || 0 }),
        });
      }
    } catch (reason) {
      setNotice(reason instanceof APIError && reason.status === 409 ? copy.player.progressConflict : copy.player.progressError);
      if (reason instanceof APIError && reason.status === 409) conflictRef.current = true;
    } finally {
      savingRef.current = false;
    }
  };
  const saveRef = useRef(save);
  saveRef.current = save;

  const qualityOptions = useMemo<{ value: number; label: string }[]>(() => {
    const options: { value: number; label: string }[] = [];
    if (levels.length) {
      levels.forEach((level, levelIndex) => {
        options.push({
          value: levelIndex,
          label: level.height ? level.height + "p" : Math.round(level.bitrate / 1000) + " kbps",
        });
      });
      return options;
    }
    session?.variants?.forEach((variant, variantIndex) => {
      if (!variant.url) return;
      options.push({
        value: variantIndex,
        label: variant.label || (variant.height ? variant.height + "p" : copy.player.quality + " " + (variantIndex + 1)),
      });
    });
    return options;
  }, [levels, session]);

  const speedChoices = useMemo(
    () => Array.from(new Set<number>([...SPEED_OPTIONS, sessionSpeed])).sort((a, b) => a - b),
    [sessionSpeed],
  );

  // Only offer providers that actually list this episode. The episode payload
  // carries the providers that returned it; an empty list means unknown, in
  // which case every enabled provider stays selectable.
  const availableProviders = useMemo(() => {
    const listed = episode.availability ?? [];
    const enabled = providers.filter((item) => item.enabled);
    if (listed.length === 0) return enabled;
    return enabled.filter((item) => listed.includes(item.id));
  }, [episode.availability, providers]);

  const providerChoices = useMemo<{ value: string; label: string }[]>(() => {
    const choices: { value: string; label: string }[] = [{ value: "auto", label: copy.player.auto }];
    availableProviders.forEach((item) => choices.push({ value: item.id, label: item.id }));
    return choices;
  }, [availableProviders]);

  // A provider kept from an earlier episode may not carry this one. Use automatic
  // selection for the request and the control instead of asking for a session
  // that cannot resolve, then normalize the stored choice.
  const effectiveProvider =
    provider === "auto" || availableProviders.some((item) => item.id === provider) ? provider : "auto";

  useEffect(() => {
    if (effectiveProvider === provider) return;
    onProviderChange(effectiveProvider);
  }, [effectiveProvider, onProviderChange, provider]);

  const activeInterval = useMemo<{ name: "intro" | "outro"; end: number } | null>(() => {
    const candidates = [
      { name: "intro" as const, value: session?.intro },
      { name: "outro" as const, value: session?.outro },
    ];
    for (const { name, value } of candidates) {
      if (!value) continue;
      if (!Number.isFinite(value.start) || !Number.isFinite(value.end)) continue;
      if (value.start < 0 || value.end <= value.start) continue;
      if (time < value.start || time >= value.end) continue;
      if (duration !== null && Number.isFinite(duration) && value.end > duration) continue;
      return { name, end: value.end };
    }
    return null;
  }, [duration, session, time]);

  let statusText = "";
  if (showCustomOverlay) {
    if (buffering) statusText = copy.player.buffering;
    else if (status === "recovering") statusText = copy.player.recovering;
    else if (status === "loading") statusText = copy.player.loading;
    else if (status === "preparing") statusText = copy.player.preparing;
    else if (status === "error") statusText = "";
    else if (ended) statusText = copy.player.completed;
    else if (!playing) statusText = copy.player.paused;
  }

  function reveal() {
    setIdle(false);
    scheduleIdle();
  }

  /* Keyboard focus must always stay visible. Pointer focus (a clicked control)
     may hide, so move it to the media surface before hiding so the next
     keystroke still targets the player. */
  function hasKeyboardFocus(): boolean {
    const active = document.activeElement;
    if (!(active instanceof HTMLElement) || !rootRef.current?.contains(active)) return false;
    if (!isInteractiveTarget(active)) return false;
    try {
      return active.matches(":focus-visible");
    } catch {
      return true;
    }
  }

  function canAutoHide(): boolean {
    const element = videoRef.current;
    if (!showCustomOverlay || !element) return false;
    if (element.paused || element.ended) return false;
    if (bufferingRef.current || menuRef.current !== null || seekingRef.current) return false;
    if (hasKeyboardFocus()) return false;
    return true;
  }

  function scheduleIdle() {
    if (idleTimerRef.current !== null) {
      window.clearTimeout(idleTimerRef.current);
      idleTimerRef.current = null;
    }
    if (!canAutoHide()) return;
    idleTimerRef.current = window.setTimeout(() => {
      idleTimerRef.current = null;
      if (!canAutoHide()) return;
      const active = document.activeElement;
      if (active instanceof HTMLElement && rootRef.current?.contains(active) && active !== videoRef.current) {
        videoRef.current?.focus({ preventScroll: true });
      }
      setIdle(true);
    }, IDLE_DELAY_MS);
  }

  function handlePointerMove() {
    const now = Date.now();
    if (now - lastRevealRef.current < 200) return;
    lastRevealRef.current = now;
    reveal();
  }

  /* A stall hint is not proof of buffering: the engine emits `stalled` and
     `waiting` on pauses that resolve themselves. Latching the flag there kept
     the status chip up and suppressed the idle timer, so the chrome never hid.
     Only a media clock that stopped moving counts, and any advance clears it. */
  function markBuffering(next: boolean) {
    if (bufferingTimerRef.current !== null) {
      window.clearTimeout(bufferingTimerRef.current);
      bufferingTimerRef.current = null;
    }
    if (!next) {
      setBuffering(false);
      return;
    }
    const element = videoRef.current;
    if (!element) return;
    const mark = element.currentTime;
    bufferingMarkRef.current = mark;
    bufferingTimerRef.current = window.setTimeout(() => {
      bufferingTimerRef.current = null;
      const media = videoRef.current;
      if (!media || media.currentTime !== bufferingMarkRef.current) return;
      setBuffering(true);
    }, BUFFERING_GRACE_MS);
  }

  function applyTextTrackModes() {
    const element = videoRef.current;
    if (!element) return;
    for (let i = 0; i < element.textTracks.length; i++) {
      element.textTracks[i].mode = tracks[i]?.id === subtitle ? "showing" : "disabled";
    }
  }

  function togglePlay() {
    const element = videoRef.current;
    if (!element || !sessionRef.current || status === "error") return;
    reveal();
    if (element.paused) {
      void element.play().catch(() => setNotice(copy.player.autoplayError));
    } else {
      element.pause();
    }
  }

  function seekTo(seconds: number) {
    const element = videoRef.current;
    if (!element || !Number.isFinite(seconds)) return;
    const limit = Number.isFinite(element.duration) ? Math.max(0, element.duration - 0.25) : seconds;
    let target = clamp(seconds, 0, limit);
    const ranges = element.seekable;
    if (ranges.length) {
      let nearest = ranges.start(0);
      let distance = Math.abs(target - nearest);
      for (let index = 0; index < ranges.length; index++) {
        const start = ranges.start(index);
        const end = ranges.end(index);
        if (target >= start && target <= end) {
          nearest = target;
          break;
        }
        for (const boundary of [start, end]) {
          if (Math.abs(target - boundary) < distance) {
            nearest = boundary;
            distance = Math.abs(target - boundary);
          }
        }
      }
      target = nearest;
    }
    element.currentTime = target;
    setTime(target);
  }

  function seekRelative(delta: number) {
    const element = videoRef.current;
    if (!element) return;
    seekTo(element.currentTime + delta);
  }

  function applyVolume(value: number) {
    const element = videoRef.current;
    if (!element) return;
    const nextValue = clamp(value, 0, 1);
    element.volume = nextValue;
    element.muted = nextValue === 0;
    setVolume(nextValue);
    setMuted(nextValue === 0);
  }

  function toggleMute() {
    const element = videoRef.current;
    if (!element) return;
    reveal();
    if (element.muted || element.volume === 0) {
      element.muted = false;
      if (element.volume === 0) element.volume = 1;
    } else {
      element.muted = true;
    }
  }

  async function toggleFullscreen() {
    const root = rootRef.current;
    const element = videoRef.current;
    reveal();
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
        return;
      }
      if (root && typeof root.requestFullscreen === "function") {
        await root.requestFullscreen();
        return;
      }
      const nativeVideo = element as (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null;
      if (nativeVideo && typeof nativeVideo.webkitEnterFullscreen === "function") {
        nativeVideo.webkitEnterFullscreen();
        return;
      }
      setNotice(copy.player.fullscreenError);
    } catch {
      setNotice(copy.player.fullscreenError);
    }
  }

  function changeQuality(value: number) {
    const element = videoRef.current;
    if (!element || !session) return;
    setQuality(value);
    if (hlsRef.current) {
      hlsRef.current.currentLevel = value;
      return;
    }
    const url = value < 0 ? session.masterUrl : session.variants?.[value]?.url;
    if (!url) return;
    const position = element.currentTime;
    const paused = element.paused;
    const speed = element.playbackRate;
    element.addEventListener(
      "loadedmetadata",
      () => {
        element.currentTime = Number.isFinite(element.duration)
          ? Math.min(position, Math.max(0, element.duration - 1))
          : position;
        element.playbackRate = speed;
        if (!paused) void element.play().catch(() => setNotice(copy.player.autoplayError));
      },
      { once: true },
    );
    element.src = mediaURL(url);
    element.load();
  }

  async function importSubtitle(file: File | undefined) {
    if (!file || !session) return;
    if (file.size > SUBTITLE_MAX_BYTES) {
      setNotice(copy.player.invalidSubtitle);
      return;
    }
    let content = "";
    try {
      content = await file.text();
    } catch {
      setNotice(copy.player.invalidSubtitle);
      return;
    }
    if (!/^\uFEFF?WEBVTT(?:\s|$)/.test(content)) {
      setNotice(copy.player.invalidSubtitle);
      return;
    }
    const data = new FormData();
    data.append("file", file);
    try {
      const track = await api<Subtitle>(`/playback/sessions/${id(session.id)}/subtitles`, {
        method: "POST",
        body: data,
      });
      setTracks((items) => [...items, track]);
      setSubtitle(track.id);
      setNotice(copy.player.subtitleImported);
    } catch (reason) {
      setNotice(errorMessage(reason, copy.player.subtitleImportError));
    }
  }

  function handleSubtitleFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    void importSubtitle(file);
  }

  function focusTrigger(kind: Exclude<PlayerMenu, null>) {
    const target =
      kind === "settings"
        ? settingsTriggerRef.current
        : kind === "captions"
          ? captionsTriggerRef.current
          : episodesTriggerRef.current;
    window.requestAnimationFrame(() => {
      const visibleTarget = target?.getClientRects().length ? target : settingsTriggerRef.current;
      visibleTarget?.focus({ preventScroll: true });
    });
  }

  function closeMenu(restoreFocus = false) {
    if (!menu) return;
    const kind = menu;
    setMenu(null);
    if (restoreFocus) focusTrigger(kind);
  }

  function openMenu(kind: Exclude<PlayerMenu, null>) {
    reveal();
    if (menu === kind) {
      closeMenu(true);
      return;
    }
    if (kind === "episodes") focusOnOpenRef.current = '[aria-current="true"]';
    setMenu(kind);
  }

  function openProviderSettings() {
    focusOnOpenRef.current = '[data-player-field="provider"]';
    setMenu("settings");
  }

  function selectEpisode(item: Episode) {
    if (!item.availability?.length) return;
    setMenu(null);
    onEpisode(item, false);
  }

  function navigateEpisode(target: Episode | undefined) {
    if (!target || !target.availability?.length) return;
    setMenu(null);
    onEpisode(target, false);
  }

  function useNativeControls() {
    setMenu(null);
    setIdle(false);
    setNativeFallback(true);
  }

  function useCustomControls() {
    setNativeFallback(false);
    setIdle(false);
    window.requestAnimationFrame(() => videoRef.current?.focus({ preventScroll: true }));
  }

  function handleStageClick(event: ReactMouseEvent<HTMLDivElement>) {
    if (nativeFallback) return;
    const target = event.target;
    if (target instanceof HTMLElement && target.closest("button, input, select, a, label")) return;
    videoRef.current?.focus({ preventScroll: true });
    if (!showCustomOverlay) return;
    if (revealOnlyRef.current || idle) {
      revealOnlyRef.current = false;
      reveal();
      return;
    }
    togglePlay();
  }

  function handlePlayerKeyDown(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.key === "Tab") {
      const panel = menu === "settings" ? settingsPanelRef.current
        : menu === "captions" ? captionsPanelRef.current
          : menu === "episodes" ? episodesPanelRef.current : rootRef.current;
      const controls = Array.from(panel?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), video[tabindex]',
      ) ?? []).filter((control) => !control.closest('[inert]') && control.getClientRects().length > 0);
      const first = controls[0];
      const last = controls[controls.length - 1];
      if (first && last && (!panel?.contains(document.activeElement) ||
        (event.shiftKey ? document.activeElement === first : document.activeElement === last))) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      }
      return;
    }
    if (nativeFallback || isInteractiveTarget(event.target)) return;
    reveal();
    if (event.key === " " || event.key === "Spacebar") {
      event.preventDefault();
      togglePlay();
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      seekRelative(SEEK_STEP_SECONDS);
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      seekRelative(-SEEK_STEP_SECONDS);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      const element = videoRef.current;
      if (element) applyVolume((element.muted ? 0 : element.volume) + VOLUME_STEP);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      const element = videoRef.current;
      if (element) applyVolume((element.muted ? 0 : element.volume) - VOLUME_STEP);
    } else if (event.key.toLowerCase() === "m") {
      event.preventDefault();
      toggleMute();
    } else if (event.key.toLowerCase() === "f") {
      event.preventDefault();
      void toggleFullscreen();
    } else if (event.key.toLowerCase() === "k") {
      event.preventDefault();
      togglePlay();
    }
  }

  function syncBuffered() {
    const element = videoRef.current;
    if (!element) return;
    const ranges = element.buffered;
    const buffered: { start: number; end: number }[] = [];
    for (let i = 0; i < ranges.length; i++) {
      if (Number.isFinite(ranges.start(i)) && Number.isFinite(ranges.end(i))) {
        buffered.push({ start: ranges.start(i), end: ranges.end(i) });
      }
    }
    setBufferedRanges(buffered);
  }

  function seekFromInput(event: ChangeEvent<HTMLInputElement>) {
    const element = videoRef.current;
    const nextTime = Number(event.target.value);
    if (!element || !Number.isFinite(nextTime)) return;
    seekTo(nextTime);
  }

  function volumeFromInput(event: ChangeEvent<HTMLInputElement>) {
    applyVolume(Number(event.target.value));
  }

  function setQualityFromInput(event: ChangeEvent<HTMLSelectElement>) {
    const value = Number(event.target.value);
    if (Number.isFinite(value)) changeQuality(value);
  }

  function setSpeedFromInput(event: ChangeEvent<HTMLSelectElement>) {
    const value = Number(event.target.value);
    if (!Number.isFinite(value)) return;
    const element = videoRef.current;
    if (element) element.playbackRate = value;
    onSpeedChange(value);
  }

  function setProviderFromInput(event: ChangeEvent<HTMLSelectElement>) {
    const value = event.target.value;
    if (!value || value === effectiveProvider) return;
    onProviderChange(value);
  }

  function setAudioFromInput(event: ChangeEvent<HTMLSelectElement>) {
    const value = Number(event.target.value);
    if (!Number.isFinite(value)) return;
    setAudioTrack(value);
    if (hlsRef.current) hlsRef.current.audioTrack = value;
  }

  function retryPlayback() {
    recoveriesRef.current = 0;
    conflictRef.current = false;
    setError("");
    setFailureKind("media");
    setStatus("loading");
    setRetry((value) => value + 1);
  }

  function tryAgainFromError() {
    retryPlayback();
  }

  useEffect(() => {
    recoveriesRef.current = 0;
    resumeRef.current = 0;
    conflictRef.current = false;
  }, [episode.id, provider, mode]);

  useEffect(() => {
    const controller = new AbortController();
    const currentGeneration = ++generationRef.current;
    let owned: PlaybackSession | null = null;
    let destroyed = false;
    let restoreMetadata: (() => void) | null = null;
    const mediaElement = videoRef.current;

    setSession(null);
    setTracks([]);
    setLevels([]);
    setAudio([]);
    setAudioTrack(-1);
    setError("");
    setFailureKind("media");
    setNotice("");
    setStatus("loading");
    setTime(0);
    setDuration(null);
    setBufferedRanges([]);
    setPlaying(false);
    setBuffering(false);
    setEnded(false);
    setQuality(-1);
    setSubtitle("off");

    async function start() {
      try {
        progressRef.current = await api<Progress | null>(`/progress/${id(episode.id)}`, { signal: controller.signal });
        const result = await api<PlaybackSession>("/playback/sessions", {
          method: "POST",
          signal: controller.signal,
          body: json({
            animeId: anime.id,
            episodeId: episode.id,
            mode,
            provider: effectiveProvider,
            quality: settings.quality,
          }),
        });
        owned = result;
        if (destroyed || currentGeneration !== generationRef.current) {
          void api(`/playback/sessions/${id(result.id)}`, { method: "DELETE" }).catch(() => {});
          return;
        }
        sessionRef.current = result;
        setSession(result);
        setTracks(result.subtitles || []);
        setStatus("preparing");

        const available = result.subtitles || [];
        const preferred =
          available.find((track) => track.language === settings.subtitleLanguage) ||
          available.find((track) => track.language === "id") ||
          available.find((track) => track.language === "en") ||
          available[0];
        setSubtitle(preferred?.id || "off");

        const element = videoRef.current;
        if (!element) return;
        const restore = () => {
          if (destroyed || currentGeneration !== generationRef.current) return;
          const position = resumeRef.current || (!progressRef.current?.completed ? progressRef.current?.positionSeconds || 0 : 0);
          if (position > 0 && Number.isFinite(element.duration)) {
            element.currentTime = Math.min(position, Math.max(0, element.duration - 1));
          }
          element.playbackRate = speedRef.current;
          setStatus("ready");
          if (Number.isFinite(element.duration)) setDuration(element.duration);
          syncBuffered();
          if (playIntentRef.current) void element.play().catch(() => setNotice(copy.player.autoplayError));
        };
        restoreMetadata = restore;
        element.addEventListener("loadedmetadata", restore, { once: true });

        if (Hls.isSupported()) {
          const engine = new Hls({ enableWorker: true, backBufferLength: 30, maxBufferLength: 30 });
          hlsRef.current = engine;
          engine.on(Hls.Events.MANIFEST_PARSED, () => {
            setLevels(engine.levels.map((level) => ({ height: level.height, bitrate: level.bitrate })));
            const wanted = Number.parseInt(settings.quality, 10);
            const chosen = Number.isFinite(wanted) ? engine.levels.findIndex((level) => level.height === wanted) : -1;
            engine.currentLevel = chosen;
            setQuality(chosen);
            setStatus("ready");
          });
          engine.on(Hls.Events.AUDIO_TRACKS_UPDATED, () => {
            setAudio(engine.audioTracks.map((track) => ({ id: track.id, name: track.name, lang: track.lang })));
            setAudioTrack(engine.audioTrack);
          });
          engine.on(Hls.Events.ERROR, (_event, data) => {
            if (!data.fatal) return;
            if (recoveriesRef.current < 1) {
              recoveriesRef.current += 1;
              resumeRef.current = element.currentTime;
              playIntentRef.current = !element.paused;
              setStatus("recovering");
              setRetry((value) => value + 1);
            } else {
              setStatus("error");
              setFailureKind("media");
              setError(copy.player.error);
              setPlaying(false);
            }
          });
          engine.loadSource(mediaURL(result.masterUrl));
          engine.attachMedia(element);
        } else if (element.canPlayType("application/vnd.apple.mpegurl")) {
          const variants = result.variants || [];
          const wanted = Number.parseInt(settings.quality, 10);
          const chosen = Number.isFinite(wanted)
            ? variants.findIndex((variant) => variant.height === wanted && Boolean(variant.url))
            : -1;
          element.src = mediaURL(chosen >= 0 ? (variants[chosen].url as string) : result.masterUrl);
          setQuality(chosen);
        } else {
          throw new Error(copy.player.error);
        }
      } catch (reason) {
        if (controller.signal.aborted) return;
        // A provider that cannot map this title is not a media failure: name it
        // and send the viewer to the provider picker instead of offering a
        // pointless retry against the same source.
        const code = reason instanceof APIError ? reason.code : "";
        const sourceFailure = code === "INVALID_INPUT" || code === "NOT_FOUND" || code === "UNSUPPORTED";
        setStatus("error");
        setFailureKind(sourceFailure ? (effectiveProvider === "auto" ? "exhausted" : "provider") : "media");
        setError(sourceFailure ? "" : errorMessage(reason, copy.player.error));
        setPlaying(false);
      }
    }

    void start();
    const interval = window.setInterval(() => void saveRef.current(), 10_000);
    const shutdown = () => void saveRef.current();
    window.addEventListener("pagehide", shutdown);

    return () => {
      destroyed = true;
      controller.abort();
      window.clearInterval(interval);
      window.removeEventListener("pagehide", shutdown);
      if (restoreMetadata) mediaElement?.removeEventListener("loadedmetadata", restoreMetadata);
      void saveRef.current();
      hlsRef.current?.destroy();
      hlsRef.current = null;
      videoRef.current?.removeAttribute("src");
      sessionRef.current = null;
      if (owned) {
        void api(`/playback/sessions/${id(owned.id)}`, { method: "DELETE", keepalive: true }).catch(() => {});
      }
    };
  }, [
    anime.id,
    episode.id,
    mode,
    effectiveProvider,
    retry,
    settings.quality,
    settings.subtitleLanguage,
  ]);

  useEffect(() => {
    const element = videoRef.current;
    if (!element) return;
    for (let i = 0; i < element.textTracks.length; i++) {
      element.textTracks[i].mode = tracks[i]?.id === subtitle ? "showing" : "disabled";
    }
  }, [subtitle, tracks]);

  useEffect(() => {
    speedRef.current = sessionSpeed;
    const element = videoRef.current;
    if (element) element.playbackRate = sessionSpeed;
  }, [episode.id, mode, provider, retry, sessionSpeed]);

  useEffect(() => {
    const element = videoRef.current;
    if (!element) return;
    const sync = () => {
      setVolume(element.volume);
      setMuted(element.muted);
    };
    element.addEventListener("volumechange", sync);
    return () => element.removeEventListener("volumechange", sync);
  }, []);

  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement === rootRef.current);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  useEffect(() => {
    setOverlayMounted(true);
    return () => setOverlayMounted(false);
  }, []);

  /* Cues are engine-painted, so their inset cannot read layout from CSS. The
     element box also covers the letterbox bars, and the engine anchors cues to
     that box, so a portrait surface would place text in the black band below
     the picture. Publish two measured lifts: one that clears the control bar,
     and one that rests just inside the picture once the chrome hides. */
  useEffect(() => {
    const bar = bottomBarRef.current;
    const video = videoRef.current;
    const root = rootRef.current;
    if (!bar || !video || !root) return;
    const publish = () => {
      const box = video.getBoundingClientRect();
      const ratio = video.videoWidth && video.videoHeight ? video.videoWidth / video.videoHeight : 0;
      const pictureHeight = ratio ? Math.min(box.height, box.width / ratio) : box.height;
      const letterbox = Math.max(0, box.height - pictureHeight) / 2;
      // Clear the seek row, not the whole bar: its top padding is scrim, and
      // lifting past it floats cues far above the controls they sit above.
      const seek = bar.querySelector(".player-seek");
      const controlsTop = seek?.getBoundingClientRect().top ?? bar.getBoundingClientRect().top;
      const clearance = Math.max(0, box.bottom - controlsTop);
      root.style.setProperty("--cue-lift", Math.round(Math.max(clearance, letterbox) + 20) + "px");
      root.style.setProperty("--cue-lift-rest", Math.round(letterbox + 12) + "px");
    };
    publish();
    const observer = new ResizeObserver(publish);
    observer.observe(bar);
    observer.observe(video);
    video.addEventListener("loadedmetadata", publish);
    return () => {
      observer.disconnect();
      video.removeEventListener("loadedmetadata", publish);
    };
    // ResizeObserver already covers the status row and fullscreen changes.
  }, [showCustomOverlay]);

  useEffect(() => {
    activePlayerCount += 1;
    const active = document.activeElement;
    if (active instanceof HTMLElement && active !== document.body && !active.closest(".player")) {
      returnFocusTarget = active;
    }
    videoRef.current?.focus({ preventScroll: true });
    return () => {
      activePlayerCount = Math.max(0, activePlayerCount - 1);
      if (activePlayerCount !== 0) return;
      const target = returnFocusTarget;
      window.requestAnimationFrame(() => {
        if (activePlayerCount === 0 && target?.isConnected) {
          target.focus({ preventScroll: true });
          returnFocusTarget = null;
        }
      });
    };
  }, []);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, []);

  useEffect(() => {
    setAnnouncement(format(copy.player.episodeChanged, { label: episode.label }));
  }, [episode.label]);

  useEffect(() => {
    setAutoplay(Boolean(settings.autoplay));
  }, [settings.autoplay]);

  useEffect(() => {
    if (!showCustomOverlay) {
      setIdle(false);
      return;
    }
    if (playing && !buffering && !menu && !seeking) {
      reveal();
    } else {
      setIdle(false);
      if (idleTimerRef.current !== null) {
        window.clearTimeout(idleTimerRef.current);
        idleTimerRef.current = null;
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [buffering, menu, playing, seeking, showCustomOverlay]);

  useEffect(() => {
    if (!menu) return;
    const panel =
      menu === "settings"
        ? settingsPanelRef.current
        : menu === "captions"
          ? captionsPanelRef.current
          : episodesPanelRef.current;
    if (!panel) return;
    const preferredSelector = focusOnOpenRef.current;
    focusOnOpenRef.current = null;
    const preferred = preferredSelector ? panel.querySelector<HTMLElement>(preferredSelector) : null;
    const fallback = panel.querySelector<HTMLElement>(
      'button:not([disabled]), select:not([disabled]), input:not([disabled])',
    );
    window.requestAnimationFrame(() => (preferred || fallback)?.focus({ preventScroll: true }));
  }, [menu]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape" || !menu) return;
      event.preventDefault();
      closeMenu(true);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [menu]);

  useEffect(() => {
    if (!menu) return;
    function onPointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node)) return;
      const panel =
        menu === "settings"
          ? settingsPanelRef.current
          : menu === "captions"
            ? captionsPanelRef.current
            : episodesPanelRef.current;
      const trigger =
        menu === "settings"
          ? settingsTriggerRef.current
          : menu === "captions"
            ? captionsTriggerRef.current
            : episodesTriggerRef.current;
      if (panel?.contains(target)) return;
      if (trigger?.contains(target)) return;
      setMenu(null);
    }
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [menu]);

  useEffect(() => {
    return () => {
      if (idleTimerRef.current !== null) window.clearTimeout(idleTimerRef.current);
      if (bufferingTimerRef.current !== null) window.clearTimeout(bufferingTimerRef.current);
    };
  }, []);

  const topBar = (
    <header className={showCustomOverlay ? "player-top player-chrome" : "player-top"}>
      <button
        type="button"
        className="player-back"
        onClick={onClose}
        aria-label={isMovie ? copy.player.backMovie : copy.player.back}
        title={isMovie ? copy.player.backMovie : copy.player.back}
      >
        <ArrowLeft aria-hidden="true" />
      </button>
      <div className="player-heading">
        <h1 className="player-title">{anime.title}</h1>
        <p className="player-identity">
          {isMovie ? (
            <span>{copy.details.movie}</span>
          ) : (
            <>
              <span>{format(copy.history.episode, { label: episode.label })}</span>
              {episode.title ? (
                <span> · {cleanEpisodeTitle(episode.title, episode.label, episode.ordinal)}</span>
              ) : null}
            </>
          )}
        </p>
      </div>
      {nativeFallback ? (
        <div className="player-top-actions">
          <button
            type="button"
            className="player-icon"
            onClick={useCustomControls}
            aria-label={copy.player.useCustomControls}
            title={copy.player.useCustomControls}
          >
            <MonitorPlay aria-hidden="true" />
          </button>
        </div>
      ) : null}
    </header>
  );

  const bottomBar = showCustomOverlay ? (
    <div className="player-bottom player-chrome" ref={bottomBarRef}>
      {statusText || activeInterval ? (
        <div className="player-context">
          {statusText ? (
            <p className="player-status" aria-hidden="true">
              {statusText === copy.player.buffering ? <span className="player-status__dot" /> : null}
              {statusText}
            </p>
          ) : null}
          {activeInterval ? (
            <button
              type="button"
              className="player-skip"
              onClick={() => seekTo(activeInterval.end)}
            >
              {activeInterval.name === "intro" ? copy.player.skipIntro : copy.player.skipEnding}
              <ChevronRight aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="player-seek">
        <div className="player-track" aria-hidden="true">
          <span className="player-track__base" />
          {bufferedRanges.map((range) => (
            <span key={range.start} className="player-track__buffered" style={{
              insetInlineStart: percent(range.start, duration) + "%",
              inlineSize: percent(range.end - range.start, duration) + "%",
            }} />
          ))}
          <span className="player-track__fill" style={{ inlineSize: playedPercent + "%" }} />
        </div>
        <input
          type="range"
          className="player-range player-range--seek"
          min={0}
          max={seekMax}
          step={0.1}
          value={seekValue}
          disabled={!durationFinite}
          aria-label={copy.player.seek}
          aria-valuetext={format(copy.player.currentTime, {
            current: formatClock(time),
            duration: formatClock(duration),
          })}
          onChange={seekFromInput}
          onPointerDown={() => {
            setSeeking(true);
            reveal();
          }}
          onPointerUp={() => setSeeking(false)}
          onPointerCancel={() => setSeeking(false)}
          onBlur={() => setSeeking(false)}
          onFocus={() => reveal()}
        />
      </div>

      <div className="player-controls">
        <button
          type="button"
          className="player-icon"
          onClick={togglePlay}
          aria-label={playing ? copy.player.pause : copy.player.play}
          title={playing ? copy.player.pause : copy.player.play}
        >
          {playing ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}
        </button>
        <button
          type="button"
          className="player-icon player-replay"
          onClick={() => seekRelative(-10)}
          aria-label={copy.player.replay}
          title={copy.player.replay}
        >
          <RotateCcw aria-hidden="true" />
        </button>
        <div className="player-volume">
          <button
            type="button"
            className="player-icon"
            onClick={toggleMute}
            aria-label={muted || volume === 0 ? copy.player.unmute : copy.player.mute}
            title={muted || volume === 0 ? copy.player.unmute : copy.player.mute}
          >
            {muted || volume === 0 ? <VolumeX aria-hidden="true" /> : <Volume2 aria-hidden="true" />}
          </button>
          <div className="player-volume__slider">
            <div className="player-track" aria-hidden="true">
              <span className="player-track__base" />
              <span className="player-track__fill" style={{ inlineSize: volumePercent + "%" }} />
            </div>
            <input
              type="range"
              className="player-range player-range--volume"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              aria-label={copy.player.volume}
              aria-valuetext={Math.round((muted ? 0 : volume) * 100) + "%"}
              onChange={volumeFromInput}
              onFocus={() => reveal()}
            />
          </div>
        </div>
        <p className="player-time">
          <span>{formatClock(time)}</span>
          <span aria-hidden="true"> / </span>
          <span>{formatClock(duration)}</span>
        </p>
        <div className="player-controls__end">
          <button
            type="button"
            className="player-icon player-next"
            onClick={() => navigateEpisode(next)}
            disabled={!nextPlayable}
            aria-label={copy.player.next}
            title={copy.player.next}
          >
            <SkipForward aria-hidden="true" />
          </button>
          <button
            ref={episodesTriggerRef}
            type="button"
            className="player-icon"
            onClick={() => openMenu("episodes")}
            aria-label={copy.player.episodes}
            title={copy.player.episodes}
            aria-haspopup="dialog"
            aria-expanded={menu === "episodes"}
            aria-controls="player-episodes-panel"
          >
            <ListVideo aria-hidden="true" />
          </button>
          <button
            ref={captionsTriggerRef}
            type="button"
            className="player-icon player-captions-trigger"
            onClick={() => openMenu("captions")}
            aria-label={copy.player.captions}
            title={copy.player.captions}
            aria-haspopup="dialog"
            aria-expanded={menu === "captions"}
            aria-controls="player-captions-panel"
          >
            <Captions aria-hidden="true" />
          </button>
          <button
            ref={settingsTriggerRef}
            type="button"
            className="player-icon"
            onClick={() => openMenu("settings")}
            aria-label={copy.player.settings}
            title={copy.player.settings}
            aria-haspopup="dialog"
            aria-expanded={menu === "settings"}
            aria-controls="player-settings-panel"
          >
            <SettingsIcon aria-hidden="true" />
          </button>
          <button
            type="button"
            className="player-icon"
            onClick={() => void toggleFullscreen()}
            aria-label={fullscreen ? copy.player.exitFullscreen : copy.player.fullscreen}
            title={fullscreen ? copy.player.exitFullscreen : copy.player.fullscreen}
          >
            {fullscreen ? <Minimize aria-hidden="true" /> : <Maximize aria-hidden="true" />}
          </button>
        </div>
      </div>

      <div
        ref={settingsPanelRef}
        id="player-settings-panel"
        className="player-popover player-popover--settings"
        role="dialog"
        aria-label={copy.player.settings}
        data-open={menu === "settings" ? "true" : "false"}
        aria-hidden={menu !== "settings"}
        inert={menu !== "settings"}
      >
        <div className="player-popover__head">
          <h2 className="player-popover__title">{copy.player.settings}</h2>
          <button type="button" className="player-icon" onClick={() => closeMenu(true)} aria-label="Close playback settings"><X aria-hidden="true" /></button>
        </div>
        <div className="player-overflow-actions">
          <button type="button" className="player-text-button" onClick={() => seekRelative(-10)}><RotateCcw aria-hidden="true" />{copy.player.replay}</button>
          <button type="button" className="player-text-button" onClick={() => navigateEpisode(next)} disabled={!nextPlayable}><SkipForward aria-hidden="true" />{copy.player.next}</button>
          <button type="button" className="player-text-button" onClick={() => openMenu("captions")}><Captions aria-hidden="true" />{copy.player.captions}</button>
        </div>
        <label className="player-field">
          <span className="player-field__label">{copy.player.quality}</span>
          <select
            value={quality}
            disabled={qualityOptions.length === 0}
            onChange={setQualityFromInput}
            data-player-field="quality"
          >
            <option value={-1}>{copy.player.auto}</option>
            {qualityOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label className="player-field">
          <span className="player-field__label">{copy.player.speed}</span>
          <select value={sessionSpeed} onChange={setSpeedFromInput} data-player-field="speed">
            {speedChoices.map((speed) => (
              <option key={speed} value={speed}>
                {speed}×
              </option>
            ))}
          </select>
        </label>
        <label className="player-field">
          <span className="player-field__label">{copy.player.provider}</span>
          <select value={effectiveProvider} onChange={setProviderFromInput} data-player-field="provider">
            {providerChoices.map((choice) => (
              <option key={choice.value} value={choice.value}>
                {choice.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div
        ref={captionsPanelRef}
        id="player-captions-panel"
        className="player-popover player-popover--captions"
        role="dialog"
        aria-label={copy.player.captions}
        data-open={menu === "captions" ? "true" : "false"}
        aria-hidden={menu !== "captions"}
        inert={menu !== "captions"}
      >
        <div className="player-popover__head">
          <h2 className="player-popover__title">{copy.player.captions}</h2>
          <button type="button" className="player-icon" onClick={() => closeMenu(true)} aria-label="Close audio and subtitles"><X aria-hidden="true" /></button>
        </div>
        <div className="player-menu-group">
          <p className="player-menu-group__label" id="player-subtitles-label">
            {copy.player.subtitles}
          </p>
          <div className="player-choices" role="group" aria-labelledby="player-subtitles-label">
            <button
              type="button"
              className="player-choice"
              aria-pressed={subtitle === "off"}
              onClick={() => setSubtitle("off")}
            >
              {subtitle === "off" ? <Check aria-hidden="true" /> : null}
              {copy.player.off}
            </button>
            {tracks.map((track) => (
              <button
                key={track.id}
                type="button"
                className="player-choice"
                aria-pressed={subtitle === track.id}
                onClick={() => setSubtitle(track.id)}
              >
                {subtitle === track.id ? <Check aria-hidden="true" /> : null}
                <span>{captionLabels.get(track.id) ?? cleanDisplayLabel(track.label)}</span>
                {track.language ? <span className="player-choice__meta">{track.language}</span> : null}
              </button>
            ))}
          </div>
          <label className="player-field">
            <span className="player-field__label">{copy.player.subtitleSize}</span>
            <select
              value={subtitleScale}
              onChange={(event) => setSubtitleScale(Number(event.target.value))}
              data-player-field="subtitle-size"
            >
              {SUBTITLE_SIZE_CHOICES.map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="player-text-button" onClick={() => fileRef.current?.click()}>
            <Upload aria-hidden="true" />
            {copy.player.import}
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".vtt,text/vtt"
            hidden
            onChange={handleSubtitleFile}
          />
        </div>
        {audio.length > 0 ? (
          <div className="player-menu-group">
            <label className="player-field">
              <span className="player-field__label">{copy.player.audio}</span>
              <select value={audioTrack} onChange={setAudioFromInput}>
                {audio.map((track) => (
                  <option key={track.id} value={track.id}>
                    {audioTrackLabel(track)}
                  </option>
                ))}
              </select>
            </label>
          </div>
        ) : null}
        {session?.hardsub ? <p className="player-popover__note">{copy.player.hardsub}</p> : null}
      </div>

      <aside
        ref={episodesPanelRef}
        id="player-episodes-panel"
        className="player-episodes"
        role="dialog"
        aria-labelledby="player-episodes-title"
        data-open={menu === "episodes" ? "true" : "false"}
        aria-hidden={menu !== "episodes"}
        inert={menu !== "episodes"}
      >
        <header className="player-episodes__head">
          <h2 className="player-episodes__title" id="player-episodes-title">
            {copy.player.episodes}
          </h2>
          <button
            type="button"
            className="player-icon"
            onClick={() => closeMenu(true)}
            aria-label={copy.player.closeEpisodes}
            title={copy.player.closeEpisodes}
          >
            <X aria-hidden="true" />
          </button>
        </header>
        <ul className="player-episodes__list">
          {episodes.map((item) => {
            const playable = Boolean(item.availability?.length);
            const current = item.id === episode.id;
            return (
              <li key={item.id}>
                <button
                  type="button"
                  className="player-episode"
                  aria-current={current ? "true" : undefined}
                  disabled={!playable}
                  onClick={() => selectEpisode(item)}
                >
                  <span className="player-episode__label">{item.label}</span>
                  <span className="player-episode__body">
                    <span className="player-episode__title">
                      {cleanEpisodeTitle(item.title, item.label, item.ordinal) ||
                        format(copy.history.episode, { label: item.label })}
                    </span>
                    {playable && current ? (
                      <span className="player-episode__meta">{copy.player.nowPlaying}</span>
                    ) : null}
                    {!playable ? <span className="player-episode__meta">{copy.player.unavailable}</span> : null}
                  </span>
                  {current ? <Check aria-hidden="true" /> : null}
                </button>
              </li>
            );
          })}
          {episodes.length === 0 ? <li className="player-episodes__empty">{copy.details.unavailable}</li> : null}
        </ul>
        <footer className="player-episodes__foot">
          <div className="player-episodes__nav">
            <button
              type="button"
              className="player-text-button"
              onClick={() => navigateEpisode(previous)}
              disabled={!previousPlayable}
            >
              <ChevronLeft aria-hidden="true" />
              {copy.player.previous}
            </button>
            <button
              type="button"
              className="player-text-button"
              onClick={() => navigateEpisode(next)}
              disabled={!nextPlayable}
            >
              {copy.player.next}
              <ChevronRight aria-hidden="true" />
            </button>
          </div>
          <label className="player-check">
            <span className="switch">
              <input
                type="checkbox"
                className="switch__input"
                checked={autoplay}
                onChange={(event) => setAutoplay(event.target.checked)}
              />
              <span className="switch__track" aria-hidden="true" />
            </span>
            {copy.preferences.autoplay}
          </label>
        </footer>
      </aside>
    </div>
  ) : null;

  const errorOverlay =
    showCustomOverlay && menu === null && status === "error" ? (
      <div className="player-error" role="alert">
        <div className="player-error__card">
          <h2 className="player-error__title">
            {failureKind === "media" ? copy.player.error : copy.player.providerTitle}
          </h2>
          <p className="player-error__body">
            {failureKind === "provider"
              ? copy.player.providerUnavailable
              : failureKind === "exhausted"
                ? copy.player.noProviders
                : copy.player.recovery}
          </p>
          {error && error !== copy.player.error ? <p className="player-error__detail">{error}</p> : null}
          <div className="player-error__actions">
            {failureKind === "media" ? (
              <button type="button" className="player-action player-action--primary" onClick={tryAgainFromError}>
                {copy.player.retry}
              </button>
            ) : null}
            <button
              type="button"
              className={"player-action" + (failureKind === "media" ? "" : " player-action--primary")}
              onClick={openProviderSettings}
            >
              {copy.player.chooseProvider}
            </button>
            <button type="button" className="player-action" onClick={onClose}>
              {isMovie ? copy.player.backMovie : copy.player.backToEpisodes}
            </button>
            <button type="button" className="player-action player-action--quiet" onClick={useNativeControls}>
              {copy.player.nativeControls}
            </button>
          </div>
        </div>
      </div>
    ) : null;

  const loadingOverlay =
    showCustomOverlay &&
    menu === null &&
    !error &&
    (status === "loading" || status === "preparing" || status === "recovering") ? (
      <div className="player-loading" role="status" aria-live="polite">
        <span className="player-spinner" aria-hidden="true" />
        <p>
          {status === "loading"
            ? copy.player.loading
            : status === "recovering"
              ? copy.player.recovering
              : copy.player.preparing}
        </p>
      </div>
    ) : null;

  return (
    <main
      ref={rootRef}
      className="player"
      aria-label={copy.player.region}
      style={{ "--subtitle-scale": subtitleScale } as CSSProperties}
      data-idle={idle ? "true" : "false"}
      data-native={nativeFallback ? "true" : "false"}
      onPointerMove={showCustomOverlay ? (event) => {
        if (event.pointerType !== "touch") handlePointerMove();
      } : undefined}
      onPointerDown={showCustomOverlay ? (event) => {
        revealOnlyRef.current = idle && Boolean((event.target as HTMLElement).closest(".player-stage"));
        reveal();
      } : undefined}
      onKeyDown={handlePlayerKeyDown}
      onFocusCapture={showCustomOverlay ? reveal : undefined}
      onBlurCapture={(event) => {
        const related = event.relatedTarget;
        if (!(related instanceof Node) || !rootRef.current?.contains(related)) scheduleIdle();
      }}
    >
      <div className="player-stage" onClick={handleStageClick}>
        <video
          ref={videoRef}
          className="player-video"
          tabIndex={0}
          aria-label={copy.player.region}
          controls={!showCustomOverlay}
          playsInline
          crossOrigin="anonymous"
          onPlay={() => {
            setPlaying(true);
            setEnded(false);
            setStatus("ready");
            setBuffering(false);
          }}
          onPause={() => {
            setPlaying(false);
            void saveRef.current();
          }}
          onWaiting={() => markBuffering(true)}
          onPlaying={() => {
            markBuffering(false);
            setStatus("ready");
          }}
          onStalled={() => markBuffering(true)}
          onCanPlay={() => markBuffering(false)}
          onSeeking={() => {
            setSeeking(true);
            reveal();
          }}
          onSeeked={() => {
            setSeeking(false);
            markBuffering(false);
          }}
          onTimeUpdate={(event) => {
            const element = event.currentTarget;
            const advanced = element.currentTime !== lastPositionRef.current;
            lastPositionRef.current = element.currentTime;
            lastDurationRef.current = Number.isFinite(element.duration) ? element.duration : null;
            setTime(element.currentTime);
            syncBuffered();
            // Advancing media is proof that buffering and seeking both ended. A
            // `waiting` or `seeking` event with no matching `playing`/`seeked`
            // would otherwise hold the chrome open for good.
            if (advanced) {
              if (bufferingRef.current) markBuffering(false);
              if (seekingRef.current) setSeeking(false);
            }
          }}
          onLoadedMetadata={(event) => {
            const element = event.currentTarget;
            setDuration(Number.isFinite(element.duration) ? element.duration : null);
            syncBuffered();
          }}
          onDurationChange={(event) => {
            const element = event.currentTarget;
            setDuration(Number.isFinite(element.duration) ? element.duration : null);
          }}
          onProgress={syncBuffered}
          onEnded={() => {
            setPlaying(false);
            setEnded(true);
            setBuffering(false);
            void saveRef.current(true);
            if (autoplay && next && next.availability?.length) onEpisode(next, true);
          }}
          onError={() => {
            if (!sessionRef.current) return;
            setStatus("error");
            setFailureKind("media");
            setError(copy.player.error);
            setPlaying(false);
          }}
          onVolumeChange={(event) => {
            setVolume(event.currentTarget.volume);
            setMuted(event.currentTarget.muted);
          }}
        >
          {tracks.map((track) => (
            <track
              key={track.id}
              kind="subtitles"
              src={mediaURL(track.url)}
              srcLang={track.language || "und"}
              label={captionLabels.get(track.id) ?? cleanDisplayLabel(track.label)}
              onLoad={applyTextTrackModes}
            />
          ))}
        </video>
      </div>

      {topBar}
      {bottomBar}
      {errorOverlay}
      {loadingOverlay}

      {notice ? (
        <div className="player-notice">
          <p className="player-notice__body" role="status">
            {notice}
          </p>
        </div>
      ) : null}

      <div className="visually-hidden" aria-live="polite" aria-atomic="true">
        {announcement}
      </div>
    </main>
  );
}
