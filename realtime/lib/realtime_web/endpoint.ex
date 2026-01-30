defmodule RealtimeWeb.Endpoint do
  @moduledoc """
  Phoenix Endpoint for WebSocket connections only.

  This service is a pure WebSocket gateway - no HTTP endpoints.
  All HTTP traffic should be handled by the Go backend.
  """

  use Phoenix.Endpoint, otp_app: :realtime

  socket "/socket", RealtimeWeb.UserSocket,
    websocket: [
      timeout: 60_000,
      compress: true,
      check_origin: false
    ],
    longpoll: false

  # Minimal plugs for WebSocket upgrade handling
  plug Plug.RequestId
  plug Plug.Telemetry, event_prefix: [:realtime, :endpoint]
end
