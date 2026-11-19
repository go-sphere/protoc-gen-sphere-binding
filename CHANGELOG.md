# Changelog

## Unreleased

## v0.0.7 (2026-11-19)

### Fixed

- `main`: update reported `-version` to `0.0.6`.
- `tagger`: validate keys in `setTag` (covering `default_auto_tags`, `default_oneof_auto_tags`, and `auto_tags`) and reject backticks in parsed `E_Tags` values.
- `replace`: unquote quoted `"..."` struct tag literals with `strconv.Unquote` and clone tag slices before sorting so caller `StructTags` are not mutated in place.

## v0.0.6 (2026-10-08)

### Changed

- **BREAKING**: well-known types (`Timestamp`, `Duration`, `wrapperspb.*Value`)
  bound to `QUERY`, `URI` or `HEADER` fail generation; the httpx decoders never
  read them from a single token, so the tag only produced a silently empty
  field. The shared `scalar_bindability` table changes with protoc-gen-sphere.
