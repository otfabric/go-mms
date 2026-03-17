# Ownership and Copy Semantics

This document describes the memory ownership model for go-mms public types.

## Core principle

**Byte slices and OID arcs are always copied at API boundaries.** Callers own the data they pass in, and own the data they receive back. Neither side can observe mutations by the other.

**Element slices (`[]*Value`) are shallow-copied.** The returned slice is independent, but the child `*Value` pointers are shared. Use `Clone()` for a deep copy.

## Value constructors (ingress)

| Constructor | Ownership |
|------------|-----------|
| `NewOctetString(data)` | Copies `data`; caller retains original |
| `NewBitString(bits)` | Copies `bits`; caller retains original |
| `NewBitStringWithLength(bits, n)` | Copies `bits` |
| `NewObjectIdentifier(oid)` | Copies `oid` slice |
| `NewArray(elements)` | Shallow copies element slice; child pointers are shared |
| `NewStructure(elements)` | Shallow copies element slice; child pointers are shared |
| All other constructors | Scalar types; no aliasing possible |

## Value accessors (egress)

| Accessor | Ownership |
|----------|-----------|
| `OctetString()` | Returns a copy |
| `BitString()` | Returns a copy |
| `ObjectIdentifier()` | Returns a copy |
| `Structure()` | Returns a new slice; child pointers are shared |
| `ArrayElements()` | Returns a new slice; child pointers are shared |
| `Get(selectors...)` | Returns a new Value; shares child pointers for non-leaf results |
| `Clone()` | Full deep copy; no aliasing with original |

## Protocol conversion (Value ↔ DataValue)

Both `valueToDataValue` and `dataValueToValue` copy all byte slices and OID arcs during conversion. Array/structure elements are recursively converted, creating independent trees.

## File services

| Function | Ownership |
|----------|-----------|
| `FileRead` | Returned `Data` is an owned copy (does not alias the transport buffer) |
| `FileReadAll` | Returns an owned `[]byte` accumulated from chunks |
| `DownloadFile` | Returns an owned `[]byte` |

## Server alternate-access patching

`applyAlternateAccessWrite` clones the current value before patching. The original value is never modified. All patched child values use `Clone()`.

`applyAlternateAccessRead` returns pointers into the value tree. The returned values should be treated as read-only; the server ensures no concurrent mutation occurs during the read handler call.

## Guidelines for callers

1. **Safe to mutate** any `[]byte` or `[]int` returned by an accessor — it is a copy.
2. **Do not mutate** child `*Value` pointers from `Structure()` or `ArrayElements()` unless you intend to modify the original Value.
3. **Use `Clone()`** when you need an independent deep copy of a Value.
4. **All constructors** accept owned data; you may freely reuse buffers after construction.
