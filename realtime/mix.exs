defmodule WarmblyWs.MixProject do
  use Mix.Project

  def project do
    [
      app: :warmbly_ws,
      version: "0.1.0",
      elixir: "~> 1.18",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {WarmblyWs.Application, []}
    ]
  end

  defp deps do
    [
      {:cowboy, "~> 2.9"},
      {:plug_cowboy, "~> 2.5"},
      {:redix, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:httpoison, "~> 2.0"},
      {:uuid, "~> 1.1.8" },
      {:ex_limit, "~> 0.1.0"}
    ]
  end
end
