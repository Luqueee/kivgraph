package com.example.basic;

public final class Service {
    private final Models model;

    public Service(Models model) {
        this.model = model;
    }

    // The accented literal is deliberate: it puts a non-ASCII character before
    // a symbol occurrence on the same line, which is the only way a wrong
    // position encoding shows up. Every column after it differs by one between
    // UTF-8 bytes and UTF-16 code units.
    public String greet() {
        return "olá " + model.name();
    }

    public static Service of(String value) {
        return new Service(new Models.Person(value));
    }
}
