import { describe, expect, it } from "vitest";
import { SelfWriteSuppress } from "../self-write-suppress";

describe("SelfWriteSuppress", () => {
  it("consumes matching write suppress", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addWrite("a.md", "H1", 2000);

    expect(suppress.consumeWrite("a.md", "H1")).toBe(true);
    expect(suppress.consumeWrite("a.md", "H1")).toBe(false);
  });

  it("does not consume write suppress with different hash", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addWrite("a.md", "H1", 2000);

    expect(suppress.consumeWrite("a.md", "H2")).toBe(false);
  });

  it("consumes matching delete suppress", () => {
    const suppress = new SelfWriteSuppress(() => 1000);
    suppress.addDelete("a.md", 2000);

    expect(suppress.consumeDelete("a.md", false)).toBe(true);
  });
});
