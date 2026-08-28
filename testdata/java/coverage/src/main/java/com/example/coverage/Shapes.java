package com.example.coverage;

import java.util.List;
import java.util.function.Function;

/** Interfaces, inheritance, generics, enums, records and nested types. */
public interface Shapes {

    double area();

    enum Kind {
        CIRCLE,
        SQUARE;

        public boolean rounded() {
            return this == CIRCLE;
        }
    }

    record Point(double x, double y) {
        public double distanceTo(Point other) {
            double dx = x - other.x();
            double dy = y - other.y();
            return Math.sqrt(dx * dx + dy * dy);
        }
    }

    abstract class Base implements Shapes {
        protected final Point origin;

        protected Base(Point origin) {
            this.origin = origin;
        }

        public abstract Kind kind();

        @Override
        public String toString() {
            return kind() + "@" + origin;
        }
    }

    final class Circle extends Base {
        private final double radius;

        public Circle(Point origin, double radius) {
            super(origin);
            this.radius = radius;
        }

        @Override
        public Kind kind() {
            return Kind.CIRCLE;
        }

        @Override
        public double area() {
            return Math.PI * radius * radius;
        }
    }

    /** A generic method and a functional value, which is a callback source. */
    static <T> List<T> mapAll(List<T> values, Function<T, T> mapper) {
        return values.stream().map(mapper).toList();
    }
}
