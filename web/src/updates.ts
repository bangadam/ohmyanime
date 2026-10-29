import { Channel, call, isMyGo } from "mygo-runtime";
import { copy } from "./copy";

/** State of the desktop update flow, as reported by the Go updater service. */
export type DesktopUpdateState = "unavailable" | "up-to-date" | "available";

export interface DesktopUpdateStatus {
  state: DesktopUpdateState;
  version?: string;
  notes?: string;
  current: string;
}

export interface DesktopUpdateProgress {
  downloaded: number;
  total: number;
}

export class UpdateUnavailableError extends Error {
  constructor() {
    super(copy.preferences.updatesUnsupported);
    this.name = "UpdateUnavailableError";
  }
}

/** Whether update UI should be offered: only inside the desktop app. */
export function updateChecksSupported(): boolean {
  return isMyGo();
}

/** Initial updater state for the Preferences page, best effort. */
export async function fetchUpdateStatus(): Promise<DesktopUpdateStatus> {
  if (!updateChecksSupported()) throw new UpdateUnavailableError();
  return (await call<DesktopUpdateStatus>("Updater.Status")) as DesktopUpdateStatus;
}

/**
 * Manual update check. Transport failures reject so the UI can offer a
 * retry; an up-to-date or disabled build resolves instead of rejecting.
 */
export async function checkForDesktopUpdates(): Promise<DesktopUpdateStatus> {
  if (!updateChecksSupported()) throw new UpdateUnavailableError();
  return (await call<DesktopUpdateStatus>("Updater.CheckForUpdates")) as DesktopUpdateStatus;
}

/**
 * Downloads and installs the pending update, reporting progress to
 * onProgress. The running app keeps working until restartDesktopApp is
 * called with the user's consent.
 */
export async function installDesktopUpdate(
  onProgress: (progress: DesktopUpdateProgress) => void,
): Promise<void> {
  if (!updateChecksSupported()) throw new UpdateUnavailableError();
  const channel = new Channel<DesktopUpdateProgress>(onProgress);
  await call("Updater.Install", channel);
}

/** Restarts into the installed update. Only call after explicit consent. */
export async function restartDesktopApp(): Promise<void> {
  if (!updateChecksSupported()) throw new UpdateUnavailableError();
  await call("Updater.Relaunch");
}
