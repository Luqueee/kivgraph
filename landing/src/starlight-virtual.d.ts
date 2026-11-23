// Starlight 0.43 stopped declaring this module, and the SocialIcons override
// reads the user config through it, as Starlight's own component does.
declare module "virtual:starlight/user-config" {
  const config: import("@astrojs/starlight/types").StarlightConfig;
  export default config;
}
