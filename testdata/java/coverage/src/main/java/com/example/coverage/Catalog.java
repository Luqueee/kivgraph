package com.example.coverage;

import java.util.ArrayList;
import java.util.List;

/**
 * Cross-file references, overloads, varargs, a static field and a lambda whose
 * body calls a method: the call has no enclosing declaration of its own, so it
 * is attributed to the method that holds the lambda.
 */
public final class Catalog {

    private static final String LABEL = "catalog";

    private final List<Shapes> entries = new ArrayList<>();

    public Catalog add(Shapes shape) {
        entries.add(shape);
        return this;
    }

    /** An overload: it must not share a stable key with the one above. */
    public Catalog add(Shapes.Point origin, double radius) {
        return add(new Shapes.Circle(origin, radius));
    }

    public double total() {
        return entries.stream().mapToDouble(Shapes::area).sum();
    }

    public List<String> describe(String... prefixes) {
        List<String> described = new ArrayList<>();
        for (String prefix : prefixes) {
            entries.forEach(shape -> described.add(prefix + LABEL + shape.area()));
        }
        return described;
    }
}
