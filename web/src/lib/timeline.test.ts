import { describe, expect, it } from "vitest";
import {
  flatten,
  formatNS,
  geometry,
  serviceColor,
  timestampNS,
  visibleRows,
} from "./timeline";
import { at, detail, span } from "./test-fixtures";
describe("precise timeline", () => {
  it("calculates shared-axis offsets and widths", () => {
    const base = timestampNS(at("0"));
    expect(geometry(span("a", "250", "850"), base, base + 1000n)).toMatchObject(
      { left: 25, width: 60, duration: 600n },
    );
    expect(geometry(span("a", "250", "850"), base, base + 2000n)).toMatchObject(
      { left: 12.5, width: 30, duration: 600n },
    );
  });
  it("retains individual ns at modern Unix epochs", () => {
    expect(timestampNS(at("123456789")) - timestampNS(at("123456788"))).toBe(
      1n,
    );
    expect(timestampNS("2026-11-12T00:00:00.1Z") % 1000000000n).toBe(
      100000000n,
    );
  });
  it("rejects invalid calendar/zone/fraction input", () => {
    for (const value of [
      "2026-02-30T00:00:00Z",
      "2026-11-12T00:00:00+01:00",
      "2026-11-12T00:00:00.1234567890Z",
    ])
      expect(() => timestampNS(value)).toThrow();
  });
  it("keeps zero and tiny durations numerically honest", () => {
    const base = timestampNS(at("0"));
    expect(geometry(span("a", "0", "0"), base, base).width).toBe(0);
    expect(
      geometry(span("b", "1", "2"), base, base + 1000000000n).duration,
    ).toBe(1n);
    expect(formatNS(0n)).toBe("0 ns");
    expect(formatNS(1n)).toBe("1 ns");
  });
  it("does not sum overlapping children", () => {
    const base = timestampNS(at("0"));
    expect(geometry(span("a", "100", "700"), base, base + 800n).width).toBe(75);
    expect(geometry(span("b", "200", "700"), base, base + 800n).width).toBe(
      62.5,
    );
  });
  it("marks reversed/outside timing and preserves raw duration", () => {
    const base = timestampNS(at("100"));
    expect(geometry(span("a", "0", "200"), base, base + 50n)).toMatchObject({
      left: 0,
      width: 100,
      clipped: true,
      duration: 200n,
    });
    expect(geometry(span("b", "200", "100"), base, base + 100n)).toMatchObject({
      width: 0,
      anomalous: true,
      duration: -100n,
    });
  });
  it("assigns stable service colors independent of order", () => {
    expect(serviceColor("gateway")).toBe(serviceColor("gateway"));
    expect(serviceColor("gateway")).not.toBe(serviceColor("payment"));
  });
});
describe("backend forest traversal", () => {
  it("keeps hierarchy and collapse local to descendants", () => {
    const root = span("root"),
      child = span("child");
    root.children = [child];
    const trace = detail([root, span("second")]);
    expect(flatten(trace).map((r) => [r.span.span_id, r.depth])).toEqual([
      ["root", 0],
      ["child", 1],
      ["second", 0],
    ]);
    expect(
      visibleRows(flatten(trace), new Set(["root"])).map((r) => r.span.span_id),
    ).toEqual(["root", "second"]);
  });
  it("retains multiple roots, orphans and disconnected nodes", () => {
    const trace = detail([span("root"), span("root2")]);
    trace.orphan_spans = [span("orphan")];
    trace.disconnected_spans = [span("cycle")];
    expect(flatten(trace).map((r) => r.group)).toEqual([
      "Roots",
      "Roots",
      "Missing parent",
      "Disconnected",
    ]);
  });
  it("terminates on cycles and renders 2000 deep nodes iteratively", () => {
    const root = span("0");
    let last = root;
    for (let i = 1; i < 2000; i++) {
      const next = span(String(i));
      last.children = [next];
      last = next;
    }
    last.children = [root];
    expect(flatten(detail([root]))).toHaveLength(2000);
  });
});
