import test from "node:test";
import assert from "node:assert/strict";
import { formatDate, formatBytes } from "./utils.ts";

test("formatDate produces deterministic UTC output for valid ISO dates", () => {
  const result = formatDate("2026-10-26T14:30:00.000Z");
  assert.equal(result, "Oct 26, 2026, 02:30 PM UTC");
});

test("formatDate returns dash for empty date", () => {
  assert.equal(formatDate(""), "-");
  assert.equal(formatDate(null), "-");
  assert.equal(formatDate(undefined), "-");
});

test("formatDate returns original string for invalid date", () => {
  assert.equal(formatDate("not-a-valid-date"), "not-a-valid-date");
});

test("formatBytes formats byte boundaries accurately", () => {
  assert.equal(formatBytes(0), "0 Bytes");
  assert.equal(formatBytes(1024), "1 KB");
  assert.equal(formatBytes(1048576), "1 MB");
  assert.equal(formatBytes(1073741824), "1 GB");
});
