// The landing is a single Node process. `astro build` prerenders every route,
// and what runs is `server.mjs` rather than the adapter's own entry point: it
// wraps the same handler so it can see every request, which is the only place
// on this site that can. An Astro middleware cannot -- measured, it sees only
// the one route that is not prerendered -- and that is what the AI crawler
// detector needs. Serving is unchanged: `server.mjs` delegates to the adapter's
// `createStandaloneHandler`, static files, the trailing-slash `301` and the 404
// included.
//
// TLS and any reverse proxy still belong to the host, not to this process.
module.exports = {
  apps: [
    {
      name: "kivgraph-landing",
      script: "./server.mjs",
      cwd: __dirname,
      exec_mode: "fork",
      instances: 1,
      autorestart: true,
      max_restarts: 10,
      env: {
        NODE_ENV: "production",
        HOST: process.env.HOST ?? "0.0.0.0",
        PORT: process.env.PORT ?? "6767",
      },
    },
  ],
};
