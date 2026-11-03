const failure = (status) => new Response("Service temporarily unavailable.", {
  status,
  headers: {
    "Cache-Control": "no-store",
    "Content-Type": "text/plain; charset=utf-8",
    "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
    "X-Content-Type-Options": "nosniff",
    "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
  },
});

export async function serveStatic(request, origins, fetchOrigin = fetch) {
  const incoming = new URL(request.url);
  const target = Object.hasOwn(origins, incoming.hostname) ? origins[incoming.hostname] : undefined;
  if (!target) return failure(404);
  if (!["GET", "HEAD", "OPTIONS"].includes(request.method)) {
    const response = failure(405);
    response.headers.set("Allow", "GET, HEAD, OPTIONS");
    return response;
  }

  const upstream = new URL(target);
  if (upstream.protocol !== "https:" || !/^[a-z0-9-]+(?:\.[a-z0-9-]+)*\.pages\.dev$/.test(upstream.hostname)
      || upstream.port || upstream.username || upstream.password || upstream.pathname !== "/"
      || upstream.search || upstream.hash) {
    return failure(503);
  }
  upstream.pathname = incoming.pathname;
  upstream.search = incoming.search;
  const headers = new Headers(request.headers);
  // These origins serve public static files and never receive application credentials.
  for (const name of ["authorization", "cookie", "proxy-authorization", "host"]) headers.delete(name);
  const response = await fetchOrigin(new Request(upstream, {
    method: request.method,
    headers,
    redirect: "manual",
  }));
  const result = new Response(response.body, response);
  const location = result.headers.get("Location");
  if (location) {
    const redirect = new URL(location, upstream);
    if (redirect.origin === upstream.origin) {
      redirect.protocol = incoming.protocol;
      redirect.host = incoming.host;
      result.headers.set("Location", redirect.href);
    }
  }
  return result;
}

export default {
  async fetch(request, env) {
    try {
      return await serveStatic(request, JSON.parse(env.PAGES_ORIGINS));
    } catch {
      return failure(503);
    }
  },
};
