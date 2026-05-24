defmodule Realtime.ErrorReporter do
  @moduledoc """
  Reports issues to Sentry and mirrors them to local logs in non-prod environments.
  """

  require Logger

  def capture_exception(exception, opts \\ []) do
    if local_logging_enabled?() do
      Logger.error(
        "[sentry-local][realtime][exception] #{Exception.message(exception)} opts=#{inspect(opts)}"
      )
    end

    Sentry.capture_exception(exception, opts)
  end

  def capture_message(message, opts \\ []) do
    if local_logging_enabled?() do
      Logger.error("[sentry-local][realtime][message] #{message} opts=#{inspect(opts)}")
    end

    Sentry.capture_message(message, opts)
  end

  defp local_logging_enabled? do
    System.get_env("APP_ENV", "dev") != "prod"
  end
end
