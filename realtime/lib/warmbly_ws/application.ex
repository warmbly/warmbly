defmodule WarmblyWs.Application do
  use Application

  def start(_type, _args) do
    children = [
      {Plug.Cowboy, scheme: :http, plug: WarmblyWs.Router, options: [
        port: Application.get_env(:warmbly_ws, :port),
        dispatch: dispatch(),
        compress: true,

      ]},
      {Redix, {Application.get_env(:warmbly_ws, :redis_url), name: :redix}},
      {Task.Supervisor, name: WarmblyWs.TaskSupervisor},
      WarmblyWs.LoggerReporter
    ]

    opts = [strategy: :one_for_one, name: WarmblyWs.Supervisor]
    Supervisor.start_link(children, opts)
  end

  defp dispatch do
    [
      {:_,
        [
          {"/", WarmblyWs.SocketHandler, []},
        ]
      }
    ]
  end
end
