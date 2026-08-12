// The site is a single Node process: `astro build` prerenders every route and
// the standalone server only serves files and the 404 route. TLS and any
// reverse proxy belong to the host, not to this process.
module.exports = {
  apps: [
    {
      name: "ladygraph-site",
      script: "./dist/server/entry.mjs",
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
