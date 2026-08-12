use vendored::Seed;

/// Starts the engine and answers the seed it was given.
pub fn start(seed: Seed) -> u32 {
    seed.value()
}
