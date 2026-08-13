/**
 * transcript_scoping.test.ts — Tests for transcript path scoping in the degradation bucket.
 *
 * The orchestrator transcript inside the unknown-run/ bucket must be scoped by
 * harness and session so two unrelated runs or harnesses cannot overwrite the
 * same file. Real run folders keep their existing fixed names exactly as before.
 *
 * Covers:
 *   - transcriptScopeSegment: the shared naming helper, exported for cross-adapter consistency
 *   - LogPaths.orchestratorRaw: scoped inside the bucket, unchanged outside it
 *   - LogPaths.orchestratorRawMeta: sidecar stays adjacent to its transcript
 *   - Two sessions in the bucket produce two distinct, non-overwriting files
 *   - Unsafe session identifiers are sanitized into safe single path components
 *   - Dots in the scope segment are replaced so sidecar derivation remains correct
 *   - Events file name, bucket folder name, and bucket nesting depth are unchanged
 */

import { describe, it, expect, beforeEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeOs from "node:os";

import {
  HARNESS,
  LogPaths,
  transcriptScopeSegment,
} from "../lib/core";

const BUCKET = "unknown-run";
const REAL_RUN_ID = "20240115T103000Z-abcd";
const WORKSPACE = nodePath.join(nodeOs.tmpdir(), "mosaic-scoping-test");

// ---------------------------------------------------------------------------
// transcriptScopeSegment — the shared naming helper
// ---------------------------------------------------------------------------

describe("transcriptScopeSegment", () => {
  describe("structure", () => {
    it("returns a non-empty string", () => {
      expect(transcriptScopeSegment("opencode", "sess-abc-123")).toBeTruthy();
    });

    it("separates harness and session with a double-underscore delimiter", () => {
      const seg = transcriptScopeSegment("opencode", "sess-abc-123");
      expect(seg).toBe("opencode__sess-abc-123");
    });

    it("is deterministic for identical inputs", () => {
      const a = transcriptScopeSegment("opencode", "sess-abc-123");
      const b = transcriptScopeSegment("opencode", "sess-abc-123");
      expect(a).toBe(b);
    });

    it("produces different segments for different session ids", () => {
      const a = transcriptScopeSegment("opencode", "sess-A");
      const b = transcriptScopeSegment("opencode", "sess-B");
      expect(a).not.toBe(b);
    });

    it("produces different segments for different harness names", () => {
      const a = transcriptScopeSegment("opencode", "sess-1");
      const b = transcriptScopeSegment("claude-code", "sess-1");
      expect(a).not.toBe(b);
    });
  });

  describe("dot replacement — keeps sidecar derivation correct", () => {
    it("replaces dots in the session component with underscores", () => {
      const seg = transcriptScopeSegment("opencode", "sess.with.dots");
      expect(seg).toBe("opencode__sess_with_dots");
    });

    it("replaces dots in the harness component with underscores", () => {
      const seg = transcriptScopeSegment("my.harness", "sess-1");
      expect(seg).not.toContain(".");
    });

    it("produces a scope segment containing no dots", () => {
      const seg = transcriptScopeSegment("opencode", "a.b.c.d");
      expect(seg).not.toContain(".");
    });
  });

  describe("sanitization", () => {
    it("does not contain a path separator in the session component", () => {
      const seg = transcriptScopeSegment("opencode", "sess/sub/path");
      expect(seg).not.toContain("/");
      expect(seg).not.toContain("\\");
    });

    it("does not contain reserved filesystem characters from the session id", () => {
      const seg = transcriptScopeSegment("opencode", 'sess<with>bad:chars"here');
      expect(seg).not.toMatch(/[<>:"/\\|?*]/);
    });

    it("does not contain reserved filesystem characters from the harness name", () => {
      const seg = transcriptScopeSegment("har<ness>", "sess-1");
      expect(seg).not.toMatch(/[<>:"/\\|?*]/);
    });
  });

  describe("canonical form for cross-adapter consistency", () => {
    it("produces the canonical form opencode__sess-abc-123 for the opencode harness", () => {
      // All four adapters must produce this same output for the same inputs.
      const seg = transcriptScopeSegment("opencode", "sess-abc-123");
      expect(seg).toBe("opencode__sess-abc-123");
    });

    it("produces the canonical form for the claude-code harness", () => {
      const seg = transcriptScopeSegment("claude-code", "sess-abc-123");
      expect(seg).toBe("claude-code__sess-abc-123");
    });
  });
});

// ---------------------------------------------------------------------------
// LogPaths.orchestratorRaw — real run folder (unchanged behavior)
// ---------------------------------------------------------------------------

describe("LogPaths.orchestratorRaw — real run folder", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("returns the fixed filename 00_orchestrator_session.raw in a real run folder", () => {
    const p = paths.orchestratorRaw(REAL_RUN_ID);
    expect(nodePath.basename(p)).toBe("00_orchestrator_session.raw");
  });

  it("returns the same path whether or not a session id is passed in a real run folder", () => {
    const withoutSession = paths.orchestratorRaw(REAL_RUN_ID);
    const withSession = paths.orchestratorRaw(REAL_RUN_ID, "sess-abc-123");
    expect(withoutSession).toBe(withSession);
  });

  it("is located inside the real run folder and not the bucket", () => {
    const p = paths.orchestratorRaw(REAL_RUN_ID);
    expect(p).toContain(REAL_RUN_ID);
    expect(p).not.toContain(BUCKET);
  });
});

// ---------------------------------------------------------------------------
// LogPaths.orchestratorRawMeta — real run folder (unchanged behavior)
// ---------------------------------------------------------------------------

describe("LogPaths.orchestratorRawMeta — real run folder", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("returns the fixed filename 00_orchestrator_session.meta.json in a real run folder", () => {
    const p = paths.orchestratorRawMeta(REAL_RUN_ID);
    expect(nodePath.basename(p)).toBe("00_orchestrator_session.meta.json");
  });

  it("returns the same path whether or not a session id is passed in a real run folder", () => {
    const withoutSession = paths.orchestratorRawMeta(REAL_RUN_ID);
    const withSession = paths.orchestratorRawMeta(REAL_RUN_ID, "sess-abc-123");
    expect(withoutSession).toBe(withSession);
  });

  it("sidecar is adjacent to its transcript in a real run folder", () => {
    const raw = paths.orchestratorRaw(REAL_RUN_ID);
    const meta = paths.orchestratorRawMeta(REAL_RUN_ID);
    expect(nodePath.dirname(raw)).toBe(nodePath.dirname(meta));
  });
});

// ---------------------------------------------------------------------------
// LogPaths.orchestratorRaw — degradation bucket (session scoping)
// ---------------------------------------------------------------------------

describe("LogPaths.orchestratorRaw — degradation bucket with session id", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("scopes the filename to include harness and session", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    const name = nodePath.basename(p);
    expect(name).toContain(HARNESS);
    expect(name).toContain("sess-abc-123");
  });

  it("produces the exact scoped filename with double-underscore separator", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    const name = nodePath.basename(p);
    expect(name).toBe(`00_orchestrator_session__${HARNESS}__sess-abc-123.raw`);
  });

  it("still ends with .raw extension", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    expect(p.endsWith(".raw")).toBe(true);
  });

  it("is located inside the bucket folder", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    expect(p).toContain(BUCKET);
  });

  it("is a direct child of the bucket folder — no extra nesting introduced", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    const bucketRoot = paths.runRoot(BUCKET);
    expect(nodePath.dirname(p)).toBe(bucketRoot);
  });

  it("replaces dots in the session id with underscores in the scope segment", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess.with.dots");
    const name = nodePath.basename(p);
    expect(name).toBe(`00_orchestrator_session__${HARNESS}__sess_with_dots.raw`);
  });

  it("sanitizes path separators in the session id", () => {
    const p = paths.orchestratorRaw(BUCKET, "sess/sub/path");
    const name = nodePath.basename(p);
    expect(name).not.toContain("/");
    expect(name).not.toContain("\\");
  });

  it("sanitizes reserved filesystem characters in the session id", () => {
    const p = paths.orchestratorRaw(BUCKET, 'sess<with>bad:chars"here');
    const name = nodePath.basename(p);
    expect(name).not.toMatch(/[<>:"/\\|?*]/);
  });
});

// ---------------------------------------------------------------------------
// LogPaths.orchestratorRaw — degradation bucket fallback (no session)
// ---------------------------------------------------------------------------

describe("LogPaths.orchestratorRaw — degradation bucket without session id", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("falls back to the unscoped name when no session id is provided", () => {
    const p = paths.orchestratorRaw(BUCKET);
    expect(nodePath.basename(p)).toBe("00_orchestrator_session.raw");
  });

  it("falls back to the unscoped name when session id is empty string", () => {
    const p = paths.orchestratorRaw(BUCKET, "");
    expect(nodePath.basename(p)).toBe("00_orchestrator_session.raw");
  });
});

// ---------------------------------------------------------------------------
// LogPaths.orchestratorRawMeta — degradation bucket (session scoping)
// ---------------------------------------------------------------------------

describe("LogPaths.orchestratorRawMeta — degradation bucket with session id", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("scopes the sidecar filename to match the transcript", () => {
    const meta = paths.orchestratorRawMeta(BUCKET, "sess-abc-123");
    const name = nodePath.basename(meta);
    expect(name).toBe(`00_orchestrator_session__${HARNESS}__sess-abc-123.meta.json`);
  });

  it("sidecar is adjacent to its transcript inside the bucket", () => {
    const raw = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    const meta = paths.orchestratorRawMeta(BUCKET, "sess-abc-123");
    expect(nodePath.dirname(raw)).toBe(nodePath.dirname(meta));
  });

  it("sidecar with a dotted session id uses the same dot-replaced scope as its transcript", () => {
    const raw = paths.orchestratorRaw(BUCKET, "sess.with.dots");
    const meta = paths.orchestratorRawMeta(BUCKET, "sess.with.dots");
    // raw: 00_orchestrator_session__opencode__sess_with_dots.raw
    // meta: 00_orchestrator_session__opencode__sess_with_dots.meta.json
    const rawStem = nodePath.basename(raw).replace(/\.raw$/, "");
    const metaStem = nodePath.basename(meta).replace(/\.meta\.json$/, "");
    expect(rawStem).toBe(metaStem);
  });

  it("sidecar falls back to the unscoped name when no session id is provided", () => {
    const meta = paths.orchestratorRawMeta(BUCKET);
    expect(nodePath.basename(meta)).toBe("00_orchestrator_session.meta.json");
  });
});

// ---------------------------------------------------------------------------
// Two sessions produce distinct files — no overwriting
// ---------------------------------------------------------------------------

describe("Two sessions in the bucket produce distinct non-overwriting files", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("two different session ids produce two different raw transcript paths", () => {
    const pA = paths.orchestratorRaw(BUCKET, "sess-A");
    const pB = paths.orchestratorRaw(BUCKET, "sess-B");
    expect(pA).not.toBe(pB);
  });

  it("two different session ids produce two different sidecar paths", () => {
    const mA = paths.orchestratorRawMeta(BUCKET, "sess-A");
    const mB = paths.orchestratorRawMeta(BUCKET, "sess-B");
    expect(mA).not.toBe(mB);
  });

  it("session A's raw path does not equal session B's meta path", () => {
    const rawA = paths.orchestratorRaw(BUCKET, "sess-A");
    const metaB = paths.orchestratorRawMeta(BUCKET, "sess-B");
    expect(rawA).not.toBe(metaB);
  });

  it("transcripts from two different harnesses for the same session do not collide", () => {
    // Both writing from the same sessionId but under different HARNESS values
    // must produce different scope segments, hence different filenames.
    const segOpencode = transcriptScopeSegment("opencode", "shared-session-id");
    const segClaudeCode = transcriptScopeSegment("claude-code", "shared-session-id");
    expect(segOpencode).not.toBe(segClaudeCode);
  });
});

// ---------------------------------------------------------------------------
// Invariants — events file, bucket name, bucket depth unchanged
// ---------------------------------------------------------------------------

describe("Invariants — events file, bucket folder, and nesting depth unchanged", () => {
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(WORKSPACE);
  });

  it("events file name inside the bucket is unchanged: 00_orchestrator_events.jsonl", () => {
    const eventsPath = paths.orchestratorEvents(BUCKET);
    expect(nodePath.basename(eventsPath)).toBe("00_orchestrator_events.jsonl");
  });

  it("bucket folder name is unknown-run", () => {
    const bucketRoot = paths.runRoot(BUCKET);
    expect(nodePath.basename(bucketRoot)).toBe("unknown-run");
  });

  it("bucket folder is a direct child of the logs root at the same depth as real run folders", () => {
    const bucketRoot = paths.runRoot(BUCKET);
    const realRunRoot = paths.runRoot(REAL_RUN_ID);
    expect(nodePath.dirname(bucketRoot)).toBe(paths.root);
    expect(nodePath.dirname(realRunRoot)).toBe(paths.root);
  });

  it("transcript path is a direct child of the bucket folder — scoping does not add depth", () => {
    const rawScoped = paths.orchestratorRaw(BUCKET, "sess-abc-123");
    const rawUnscoped = paths.orchestratorRaw(BUCKET);
    const bucketRoot = paths.runRoot(BUCKET);
    expect(nodePath.dirname(rawScoped)).toBe(bucketRoot);
    expect(nodePath.dirname(rawUnscoped)).toBe(bucketRoot);
  });

  it("events file is a direct child of the bucket folder after this change", () => {
    const eventsPath = paths.orchestratorEvents(BUCKET);
    const bucketRoot = paths.runRoot(BUCKET);
    expect(nodePath.dirname(eventsPath)).toBe(bucketRoot);
  });
});
