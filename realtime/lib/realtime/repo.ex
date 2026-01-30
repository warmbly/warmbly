defmodule Realtime.Repo do
  @moduledoc """
  Ecto repository for accessing the PostgreSQL database.

  Used primarily for API key validation and user lookups.
  """

  use Ecto.Repo,
    otp_app: :realtime,
    adapter: Ecto.Adapters.Postgres
end
