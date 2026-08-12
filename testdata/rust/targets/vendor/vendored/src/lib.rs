/// A seed the consumer builds and reads.
pub struct Seed(u32);

impl Seed {
    pub fn new(value: u32) -> Self {
        Seed(value)
    }

    pub fn value(&self) -> u32 {
        self.0
    }
}
