import os from 'os';
import path from 'path';
import { spawn } from 'child_process';
import { existsSync } from 'fs';

// Keep track of the tauri-driver child process
let tauriDriver: ReturnType<typeof spawn>;
let sessionEnded = false;

function resolveBinaryPath(): string {
  const desktopRoot = path.resolve(__dirname);
  const binaryName = process.platform === 'win32' ? 'radioledger-desktop.exe' : 'radioledger-desktop';

  const releasePath = path.join(desktopRoot, 'src-tauri', 'target', 'release', binaryName);
  const debugPath = path.join(desktopRoot, 'src-tauri', 'target', 'debug', binaryName);

  if (existsSync(releasePath)) return releasePath;
  if (existsSync(debugPath)) return debugPath;

  // Prefer release path for error output when neither exists
  return releasePath;
}

function closeTauriDriver() {
  sessionEnded = true;
  if (tauriDriver) {
    tauriDriver.kill();
  }
}

export const config: WebdriverIO.Config = {
  hostname: '127.0.0.1',
  port: 4444,
  path: '/',

  specs: ['./test/**/*.spec.ts'],
  maxInstances: 1,

  capabilities: [
    {
      maxInstances: 1,
      'tauri:options': {
        application: resolveBinaryPath(),
      },
    } as WebdriverIO.Capabilities,
  ],

  reporters: ['spec'],
  framework: 'mocha',

  mochaOpts: {
    ui: 'bdd',
    timeout: 60_000,
  },

  onPrepare() {
    const resolvedPath = resolveBinaryPath();
    console.log('[wdio] Binary path:', resolvedPath);
    if (!existsSync(resolvedPath)) {
      throw new Error(
        `[wdio] Tauri binary not found at ${resolvedPath}. Build first with "pnpm run tauri:build -- --debug --no-bundle".`,
      );
    }
  },

  beforeSession() {
    const cargoHome = process.env.CARGO_HOME || path.join(os.homedir(), '.cargo');
    const tauriDriverName = process.platform === 'win32' ? 'tauri-driver.exe' : 'tauri-driver';
    const driverBin = path.resolve(cargoHome, 'bin', tauriDriverName);

    tauriDriver = spawn(driverBin, [], {
      stdio: [null, process.stdout, process.stderr],
    });

    tauriDriver.on('error', (error: Error) => {
      console.error('[wdio] tauri-driver error:', error.message);
      console.error('[wdio] Make sure tauri-driver is installed: cargo install tauri-driver');
      process.exit(1);
    });

    tauriDriver.on('exit', (code: number | null) => {
      if (!sessionEnded) {
        console.error('[wdio] tauri-driver exited unexpectedly with code:', code);
        process.exit(1);
      }
    });
  },

  afterSession() {
    closeTauriDriver();
  },

  onComplete() {
    closeTauriDriver();
  },
};
