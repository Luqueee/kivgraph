package com.example.basic;

public interface Models {
    String name();

    class Person implements Models {
        private final String value;

        public Person(String value) {
            this.value = value;
        }

        @Override
        public String name() {
            return value;
        }
    }
}
