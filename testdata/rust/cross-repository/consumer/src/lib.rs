use support::{double, Value};

/// run reaches a crate another repository provides.
pub fn run(seed: i32) -> Value {
    Value {
        inner: double(seed),
    }
}
