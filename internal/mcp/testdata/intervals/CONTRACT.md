# Interval normalization

Implement `Normalize(input []Interval) ([]Interval, error)` in `intervals.go`.
An Interval includes every integer from Start through End, inclusive.

- Return intervals sorted by Start, merging every overlap and adjacency.
- Do not change the input slice or any of its elements, even on errors.
- Empty input returns an empty result and no error; nil and non-nil empty are
  both acceptable outputs.
- If any Start is greater than End, return nil and ErrInvalidInterval.
- Handle the full machine-int range without overflowing adjacency checks.
- Use O(n log n) time and O(n) additional space. Do not enumerate range members.
- Preserve the exported Interval type, error sentinel, and function signature.
- Change only intervals.go. Acceptance tests and this contract are immutable.

First inspect the contract and tests, then implement or repair the function.
Run `go test ./...`. Git history and worktree management are forbidden.
