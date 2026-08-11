//! A provider crate: everything the consumer reaches lives here.

pub mod shapes;

/// Value is the type the consumer builds.
pub struct Value {
    pub inner: i32,
}

/// double is the exported function the consumer calls.
pub fn double(value: i32) -> i32 {
    value * 2
}

fn private_helper() -> i32 {
    7
}

/// helper_user calls a private function of its own crate.
pub fn helper_user() -> i32 {
    private_helper()
}
