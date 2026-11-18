# ADR 0129: Dart references follow what the Analysis Server navigates

## Status

Accepted.

## Context

The language-recall benchmark measured Dart on `dart-lang/collection`: file
recall `49.0 %` over non-test and test files, `64.9 %` on non-test files alone,
and `25` answers that said `COMPLETE` and missed a file. Three causes, each
reproduced on that repository:

1. **A file with a non-ASCII character had no references at all.** The loader
   asks the Analysis Server for `analysis.getNavigation` over `length` bytes of
   the file, and the server counts UTF-16 code units. A comment with an accent
   or a dash made the byte count larger, and the server answered an empty
   navigation without an error. `comparators.dart` and
   `union_set_controller.dart` (an `—` in a comment) published none.
2. **Building an instance was not a use of the class.** A class with an
   explicit constructor has the call resolved to the constructor, so the class
   had no reference from the file that instantiates it.
3. **Doc links were published as relations.** The server navigates `[name]` in a
   `///` comment like code. The reference was sourced at the module, and the
   keyword scan that classifies a reference read the sentence around it:
   "compatible with [hash]" became a module `EMBEDS` the function, and
   "extends [hash]" an `EXTENDS`.

## Decision

- The navigation request carries the length in UTF-16 code units.
- A reference whose target is a constructor is also published against the class
  the constructor builds, as `CALLS_DIRECT`, from the same evidence. The
  constructor edge stays. A constructor of the class does not reference its own
  class, and a class with an implicit constructor still gets one edge, because
  the server resolves that call to the class.
- A navigation region on a line that is a `//` or `///` comment publishes
  nothing. Only a line that starts with the marker counts, so a URL inside a
  string on a line of code does not hide what follows it.

## Consequences

- Measured on `collection` with the same sample: file recall on non-test files
  `64.9 %` to `98.7 %`, answers that said `COMPLETE` and missed a file `25` to
  `1`, file precision `76.4 %` to `92.5 %` (default) and `88.4 %` with
  `dart.include_tests: true`.
- `find_references` on a Dart class now includes its instantiations, and no
  longer includes doc links. Anyone who relied on a doc link to find a
  declaration's mentions loses them; the compiler never treated them as uses.
- Still missed: an explicit extension application, `IterableExtension(this)`:
  the server gives no navigation target for the extension name there.
- Block doc comments (`/** */`) are not recognised and keep their links.
- Existing Dart graphs need `kivgraph index --full`.
