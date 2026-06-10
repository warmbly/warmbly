# Contributing

Bug reports and pull requests are welcome. Keep changes scoped to one logical
change and open an issue first for larger design or product changes.

Before opening a pull request, run the checks for the part of the repo you
touched:

- Go: `make fmt` and `make lint`
- Dashboard/admin/site: `pnpm typecheck` and `pnpm lint` in the affected tree
- Rust tracking service: `cargo fmt` and `cargo test`
- Realtime service: `mix format` and `mix test`

When changing backend or worker behavior, keep the control-plane and
execution-plane split intact. Workers should not connect directly to Postgres;
route stateful workflows through the backend or event bus instead.

When adding an external dependency, keep self-hostability intact. Any required
third-party service needs an open-source or self-hosted path.
