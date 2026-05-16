// Unit tests for the onboarding deep-link handshake. We stub
// SecretStorage + globalState with in-memory maps and stand up a
// tiny http.Server to play the role of /extension-tokens/exchange.

import * as http from "http";
import {
  storeAPIKey,
  readAPIKey,
  forgetAPIKey,
  legacyMigrate,
  handleCallback,
  isOnboarded,
  OnboardingDeps,
} from "./onboarding";

// Minimal fakes for the vscode surface this module touches.
class FakeSecrets {
  private bag = new Map<string, string>();
  changeListeners: Array<(e: { key: string }) => void> = [];
  async get(k: string): Promise<string | undefined> {
    return this.bag.get(k);
  }
  async store(k: string, v: string): Promise<void> {
    this.bag.set(k, v);
    this.changeListeners.forEach((fn) => fn({ key: k }));
  }
  async delete(k: string): Promise<void> {
    this.bag.delete(k);
    this.changeListeners.forEach((fn) => fn({ key: k }));
  }
}

class FakeMemento {
  data = new Map<string, unknown>();
  get<T>(k: string, fallback: T): T {
    return (this.data.get(k) as T) ?? fallback;
  }
  async update(k: string, v: unknown) {
    this.data.set(k, v);
  }
}

function makeDeps(): OnboardingDeps {
  return {
    secrets: new FakeSecrets() as unknown as OnboardingDeps["secrets"],
    globalState: new FakeMemento() as unknown as OnboardingDeps["globalState"],
    log: { appendLine: jest.fn() } as unknown as OnboardingDeps["log"],
  };
}

// `jest.mock("vscode", ...)` is unnecessary here — jest.config.json's
// moduleNameMapper already redirects "vscode" → test/mocks/vscode. The
// mock is loaded automatically when onboarding.ts imports it.

describe("onboarding storage", () => {
  it("store + read round-trip", async () => {
    const d = makeDeps();
    await storeAPIKey(d, "csk_live_abcdef");
    expect(await readAPIKey(d)).toBe("csk_live_abcdef");
    expect(isOnboarded(d)).toBe(true);
  });

  it("forget wipes both key + flag", async () => {
    const d = makeDeps();
    await storeAPIKey(d, "csk_live_abcdef");
    await forgetAPIKey(d);
    expect(await readAPIKey(d)).toBe("");
    expect(isOnboarded(d)).toBe(false);
  });
});

describe("onboarding.legacyMigrate", () => {
  it("returns false when settings has no plaintext key", async () => {
    const vscodeMock = require("../test/mocks/vscode");
    vscodeMock.workspace.getConfiguration.mockReturnValueOnce({
      get: (_k: string, fb: unknown) => fb ?? "",
      update: jest.fn(),
    });
    const d = makeDeps();
    expect(await legacyMigrate(d)).toBe(false);
  });

  it("moves a plaintext settings key into SecretStorage and clears it", async () => {
    const vscodeMock = require("../test/mocks/vscode");
    const update = jest.fn().mockResolvedValue(undefined);
    vscodeMock.workspace.getConfiguration.mockReturnValueOnce({
      get: (k: string) => (k === "apiKey" ? "csk_live_legacyvalue" : ""),
      update,
    });
    const d = makeDeps();
    expect(await legacyMigrate(d)).toBe(true);
    expect(await readAPIKey(d)).toBe("csk_live_legacyvalue");
    // update("apiKey", undefined, …) was called at least once with the
    // clear sentinel. We assert the first two args by hand because
    // expect.anything() rejects literal `undefined` for the target arg.
    expect(update).toHaveBeenCalled();
    const clears = update.mock.calls.filter(
      (c: unknown[]) => c[0] === "apiKey" && c[1] === undefined,
    );
    expect(clears.length).toBeGreaterThanOrEqual(1);
  });

  it("does not overwrite an already-stored keychain entry", async () => {
    const vscodeMock = require("../test/mocks/vscode");
    vscodeMock.workspace.getConfiguration.mockReturnValueOnce({
      get: (k: string) => (k === "apiKey" ? "csk_live_legacyvalue" : ""),
      update: jest.fn().mockResolvedValue(undefined),
    });
    const d = makeDeps();
    await storeAPIKey(d, "csk_live_keychain_was_already_here");
    await legacyMigrate(d);
    expect(await readAPIKey(d)).toBe("csk_live_keychain_was_already_here");
  });
});

describe("onboarding.handleCallback", () => {
  let server: http.Server;
  let baseURL: string;
  let lastBody: any;
  let nextResponse: { status: number; body: string };

  beforeAll((done) => {
    server = http.createServer((req, res) => {
      let raw = "";
      req.on("data", (c) => (raw += c));
      req.on("end", () => {
        lastBody = raw ? JSON.parse(raw) : null;
        res.statusCode = nextResponse.status;
        res.setHeader("Content-Type", "application/json");
        res.end(nextResponse.body);
      });
    });
    server.listen(0, () => {
      const addr = server.address() as { port: number };
      baseURL = `http://localhost:${addr.port}`;
      done();
    });
  });
  afterAll(() => server.close());

  function uri(query: string) {
    // Minimal vscode.Uri shape — handleCallback only reads `query`.
    return { query } as unknown as import("vscode").Uri;
  }

  it("rejects missing state", async () => {
    const d = makeDeps();
    await handleCallback(uri(""), d, { controlURL: baseURL });
    expect(await readAPIKey(d)).toBe("");
  });

  it("rejects state mismatch", async () => {
    const d = makeDeps();
    await (d.secrets as unknown as FakeSecrets).store(
      "codesec.onboarding.state",
      "remembered-state-aaaa1111",
    );
    await handleCallback(uri("state=someoneelses"), d, { controlURL: baseURL });
    expect(await readAPIKey(d)).toBe("");
  });

  it("stores the plaintext key on a matching state + 200", async () => {
    const d = makeDeps();
    const state = "abcdef0123456789abcdef0123456789";
    await (d.secrets as unknown as FakeSecrets).store(
      "codesec.onboarding.state",
      state,
    );
    nextResponse = {
      status: 200,
      body: JSON.stringify({
        api_key: "csk_live_freshfreshfresh",
        org_slug: "acme",
        key_name: "VS Code on alice-mbp",
      }),
    };
    await handleCallback(uri("state=" + state), d, { controlURL: baseURL });
    expect(lastBody).toEqual({ state });
    expect(await readAPIKey(d)).toBe("csk_live_freshfreshfresh");
    // Remembered state must be cleared so a leaked callback can't be replayed.
    expect(
      await (d.secrets as unknown as FakeSecrets).get("codesec.onboarding.state"),
    ).toBeUndefined();
  });

  it("does NOT store anything on a non-200 exchange", async () => {
    const d = makeDeps();
    const state = "00000000000000000000000000000000";
    await (d.secrets as unknown as FakeSecrets).store(
      "codesec.onboarding.state",
      state,
    );
    nextResponse = { status: 410, body: '{"detail":"already consumed"}' };
    await handleCallback(uri("state=" + state), d, { controlURL: baseURL });
    expect(await readAPIKey(d)).toBe("");
  });

  it("clears the remembered state even on a failed exchange so replay is impossible", async () => {
    const d = makeDeps();
    const state = "ffffffffffffffffffffffffffffffff";
    await (d.secrets as unknown as FakeSecrets).store(
      "codesec.onboarding.state",
      state,
    );
    nextResponse = { status: 500, body: '{"detail":"boom"}' };
    await handleCallback(uri("state=" + state), d, { controlURL: baseURL });
    expect(
      await (d.secrets as unknown as FakeSecrets).get("codesec.onboarding.state"),
    ).toBeUndefined();
  });
});
