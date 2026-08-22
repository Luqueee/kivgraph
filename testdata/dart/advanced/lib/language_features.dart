library language_features;

enum VehicleKind { car, bike }

sealed class Result<T> {
  const Result();
}

final class Success<T> extends Result<T> {
  final T value;

  const Success(this.value);
}

typedef Mapper<T, R> = R Function(T value);

extension type UserId(int value) {
  String asText() => value.toString();
}

String fallback() => 'fallback';

String runWith(String Function() handler) => handler();

// `fallback` is an operand of a comparison here and an argument below. Both
// shapes put a `(` before it and a `)` after it, and both used to publish
// PASSES_AS_CALLBACK.
bool prefersFallback(String Function() handler) => handler == fallback;

String choose(bool useFallback) => useFallback ? runWith(fallback) : fallback();

({String name, int wheels}) describe(VehicleKind kind) {
  return switch (kind) {
    VehicleKind.car => (name: 'car', wheels: 4),
    VehicleKind.bike => (name: 'bike', wheels: 2),
  };
}
