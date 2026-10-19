// Run: node src/lib/jsonDiff.check.mjs
/* oxlint-disable no-console */
import assert from 'node:assert/strict';
import { diffValues, parseJsonish } from './jsonDiff.js';

const before = parseJsonish('{"ami_id": "ami-1", "tags": {"a": 1}, "errors": ["RequestID: 1a3b"]}');
const after = parseJsonish('{"errors":["RequestID: 988b"],"tags":{"a":1},"ami_id":"ami-1"}');
assert.deepEqual(diffValues(before, after), [{ path: ['errors', 0], before: 'RequestID: 1a3b', after: 'RequestID: 988b' }]);
assert.deepEqual(diffValues({ x: 1 }, { x: 1 }), []);
assert.deepEqual(diffValues({ a: 1 }, { a: 1, b: 2 }), [{ path: ['b'], before: undefined, after: 2 }]);
assert.deepEqual(diffValues([1], [1, 2]), [{ path: [], before: [1], after: [1, 2] }]);
assert.equal(parseJsonish('plain text'), 'plain text');
console.log('jsonDiff ok');
