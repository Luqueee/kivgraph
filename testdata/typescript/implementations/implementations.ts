import type { Reader as Readable, NamedReader, Box } from './contracts.js';
export class Declared implements Readable { read(): string { return 'value'; } }
export class Structural { read(): string { return 'value'; } }
export class Inherited extends Structural { name = 'named'; }
export class Wrong { read(): number { return 1; } }
export class StringBox implements Box<string> { get(): string { return ''; } }
export class Generic<T> { constructor(private value: T) {} get(): T { return this.value; } }
export const instance = new Generic<string>('value');
export abstract class Abstract implements Readable { abstract read(): string; }
export class Concrete extends Abstract { read(): string { return ''; } }
export const named: NamedReader = new Inherited();
