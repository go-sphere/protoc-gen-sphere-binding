# Changelog

## Unreleased

## v0.0.6 (2026-10-08)

### Changed

- **BREAKING**: well-known types (`Timestamp`, `Duration`, `wrapperspb.*Value`)
  bound to `QUERY`, `URI` or `HEADER` fail generation; the httpx decoders never
  read them from a single token, so the tag only produced a silently empty
  field. The shared `scalar_bindability` table changes with protoc-gen-sphere.
