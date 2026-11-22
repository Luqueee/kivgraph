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

## Amendment: what is not a failure, and a library once

A use whose target is not published was one `DART_TARGET_NOT_INDEXED` row, with
the absolute path of the target's file. On one application that was `217689`
failures, and two kinds of them said nothing.

- A parameter, a local variable, a type parameter or an import prefix of the
  repository is not published because it means nothing outside its
  declaration. A use of one is no longer recorded. They were `45597` of the
  `71566` rows of that application's own code.
- A target outside the repository -- the SDK, a package nobody registered --
  is recorded once for each declaration that uses it, library and kind of
  target, at the first use, and the library is named the way an import spells
  it: `dart:core`, `package:flutter`. The path was that of one machine.

The same application records `47187` rows. A target inside the repository
that is a declaration and was not found is recorded as before, one row for
each use.

`Extension(receiver).member` is a use of the extension (ADR 0151).
