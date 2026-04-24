import { describe, it, expect } from "vitest";
import { sha256 } from "../hash";

describe("sha256", () => {
  it("returns 64-char hex string for text input", async () => {
    const result = await sha256("hello");
    expect(result).toHaveLength(64);
    expect(result).toMatch(/^[0-9a-f]+$/);
  });

  it("returns consistent hash for same input", async () => {
    const a = await sha256("test content");
    const b = await sha256("test content");
    expect(a).toBe(b);
  });

  it("returns different hashes for different inputs", async () => {
    const a = await sha256("content a");
    const b = await sha256("content b");
    expect(a).not.toBe(b);
  });

  it("hashes ArrayBuffer", async () => {
    const buf = new TextEncoder().encode("hello").buffer as ArrayBuffer;
    const result = await sha256(buf);
    expect(result).toHaveLength(64);
  });

  it("text and equivalent ArrayBuffer produce same hash", async () => {
    const text = "consistency check";
    const buf = new TextEncoder().encode(text).buffer as ArrayBuffer;
    const fromText = await sha256(text);
    const fromBuf = await sha256(buf);
    expect(fromText).toBe(fromBuf);
  });

  it("known sha256 of empty string", async () => {
    const result = await sha256("");
    expect(result).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
  });
});
