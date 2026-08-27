// Package executable answers what a platform means by "a program".
//
// Two questions, and both have a POSIX answer this repository had inlined in
// five places: a program is a file with an execute bit, and it is stored under
// the name it is called by. Windows agrees with neither. It has no execute
// bit -- Go reports every regular file as 0666, so a permission test for one
// answers "not a program" about every program on the disk -- and it decides
// what it will run from the extension, which is also part of the file name a
// caller has to look for.
//
// Neither difference is deep, which is exactly why it kept being written out
// by hand and getting it wrong was silent: the checks did not fail, they
// answered no.
package executable
