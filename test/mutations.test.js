import { describe, it } from 'node:test';
import { isMutationCommand, MUTATION_COMMANDS } from '../src/mutations.js';
import assert from 'node:assert';

describe('mutations.js', () => {
  it('should have MUTATION_COMMANDS exported as an array', () => {
    assert.ok(Array.isArray(MUTATION_COMMANDS));
    assert.ok(MUTATION_COMMANDS.length > 0);
  });

  it('should detect incidents create as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['incidents', 'create'] }), true);
  });

  it('should not detect incidents list as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['incidents', 'list'] }), false);
  });

  it('should detect dev eval as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['dev', 'eval'] }), true);
  });

  it('should not detect profiles use as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['profiles', 'use', 'foo'] }), false);
  });

  it('should detect records delete as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['records', 'delete'] }), true);
  });

  it('should not detect help as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['help'] }), false);
  });

  it('should not detect setup as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['setup'] }), false);
  });

  it('should detect dev updatesets set as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['dev', 'updatesets', 'set'] }), true);
  });

  it('should not detect dev updatesets list as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['dev', 'updatesets', 'list'] }), false);
  });

  it('should detect dev scopes set as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['dev', 'scopes', 'set'] }), true);
  });

  it('should not detect dev scopes list as mutation', () => {
    assert.strictEqual(isMutationCommand({ _: ['dev', 'scopes', 'list'] }), false);
  });

  it('should handle empty argv', () => {
    assert.strictEqual(isMutationCommand({ _: [] }), false);
  });
});
