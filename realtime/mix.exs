defmodule Realtime.MixProject do
  use Mix.Project

  def project do
    [
      app: :realtime,
      version: "0.1.0",
      elixir: "~> 1.18",
      listeners: [Phoenix.CodeReloader],
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger, :runtime_tools],
      mod: {Realtime.Application, []}
    ]
  end

  defp deps do
    [
      # Phoenix
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:plug_cowboy, "~> 2.7"},
      # cowlib has no patched release for EEF-CVE-2026-43966/43969; cowboy >= 2.16
      # rejects CR/LF response header values before the wire, so keep this floor.
      {:cowboy, "~> 2.16"},
      {:jason, "~> 1.4"},

      # Google Pub/Sub
      {:broadway, "~> 1.0"},
      {:broadway_cloud_pub_sub, "~> 0.9"},
      {:goth, "~> 1.4"},

      # Database
      {:ecto_sql, "~> 3.10"},
      {:postgrex, "~> 0.17"},

      # Redis
      {:redix, "~> 1.3"},

      # Authentication
      {:jose, "~> 1.11"},

      # Error tracking. Finch is the HTTP client (already pulled in by goth and
      # broadway_cloud_pub_sub); hackney was dropped to clear CVE-2026-47071.
      {:sentry, "~> 10.0"},
      {:finch, "~> 0.21"},

      # Utilities
      {:uuid, "~> 1.1"}
    ]
  end
end
