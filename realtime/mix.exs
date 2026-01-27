defmodule Realtime.MixProject do
  use Mix.Project

  def project do
    [
      app: :realtime,
      version: "0.1.0",
      elixir: "~> 1.18",
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

      # Error tracking
      {:sentry, "~> 10.0"},
      {:hackney, "~> 1.8"},

      # Utilities
      {:uuid, "~> 1.1"}
    ]
  end
end
