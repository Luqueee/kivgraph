// The build script is a crate of its own with a root module and a `main` of
// its own, and its moniker names this package: the analyzer emits the same
// symbol for it and for the binary.
fn main() {
    println!("cargo:rerun-if-changed=build.rs");
}
