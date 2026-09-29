import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (file) => readFileSync(new URL(file, import.meta.url), 'utf8')

test('FMS bridge management page keeps the mirror inspection and sync entrypoints', () => {
  const source = read('./FMSBridge.vue')
  assert.match(source, /getFMSBridgeOverview/)
  assert.match(source, /getFMSBridgeMirrorDetail/)
  assert.match(source, /triggerFMSBridgeMirrorReconcile/)
  assert.match(source, /triggerFMSBridgeReconcile/)
  assert.match(source, /SettingDrawer/)
  assert.match(source, /retrieval_units/)
  assert.match(source, /failureEntries/)
  assert.doesNotMatch(source, /vector|qdrant|embedding/i)
})
