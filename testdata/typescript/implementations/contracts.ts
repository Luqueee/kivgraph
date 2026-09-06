export interface Reader { read(): string; }
export interface NamedReader extends Reader { name: string; }
export interface Box<T> { get(): T; }
export type TextBox = Box<string>;
