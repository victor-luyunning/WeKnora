<template>
  <div class="fms-bridge">
    <header class="section-header fms-bridge__header">
      <div>
        <h2>{{ t('system.globalSettings.fmsBridge.title') }}</h2>
        <p class="section-description">{{ t('system.globalSettings.fmsBridge.description') }}</p>
      </div>
      <div class="fms-bridge__actions">
        <t-button theme="primary" :loading="syncing" :disabled="!overview?.enabled" @click="startSync">
          <template #icon><t-icon name="play-circle" /></template>
          {{ t('system.globalSettings.fmsBridge.actions.sync') }}
        </t-button>
        <button
          type="button"
          class="rq-refresh"
          :disabled="loading"
          :title="t('system.globalSettings.fmsBridge.actions.refresh')"
          :aria-label="t('system.globalSettings.fmsBridge.actions.refresh')"
          @click="reload"
        >
          <t-icon :name="loading ? 'loading' : 'refresh'" :class="{ 'rq-refresh-spin': loading }" />
        </button>
      </div>
    </header>

    <div v-if="loading && !overview" class="fms-bridge__loading" aria-live="polite">
      <t-skeleton animation="gradient" :row-col="[{ width: '28%', height: '28px' }, { width: '75%', height: '14px' }]" />
      <t-skeleton animation="gradient" :row-col="[{ width: '100%', height: '48px' }, { width: '100%', height: '48px' }]" />
    </div>

    <t-alert v-else-if="error" theme="error" class="fms-bridge__alert" :message="error">
      <template #operation><t-button size="small" @click="reload">{{ t('system.globalSettings.fmsBridge.actions.retry') }}</t-button></template>
    </t-alert>

    <template v-else-if="overview">
      <t-alert v-if="!overview.enabled" theme="warning" class="fms-bridge__alert">
        <template #message>
          <strong>{{ t('system.globalSettings.fmsBridge.notConfigured.title') }}</strong>
          <p>{{ overview.configuration_issue || t('system.globalSettings.fmsBridge.notConfigured.description') }}</p>
        </template>
      </t-alert>

      <section class="fms-bridge__metrics" :aria-label="t('system.globalSettings.fmsBridge.summary.title')">
        <div class="fms-bridge__metric">
          <span>{{ t('system.globalSettings.fmsBridge.summary.mirrors') }}</span>
          <strong>{{ overview.mirror_count }}</strong>
        </div>
        <div class="fms-bridge__metric">
          <span>{{ t('system.globalSettings.fmsBridge.summary.lastStatus') }}</span>
          <strong :class="statusClass(overview.latest_run?.status)">{{ runStatus(overview.latest_run?.status) }}</strong>
        </div>
        <div class="fms-bridge__metric">
          <span>{{ t('system.globalSettings.fmsBridge.summary.lastSuccess') }}</span>
          <strong>{{ overview.latest_run?.summary.succeeded ?? 0 }}</strong>
        </div>
        <div class="fms-bridge__metric" :class="{ 'fms-bridge__metric--warning': (overview.latest_run?.summary.failed ?? 0) > 0 }">
          <span>{{ t('system.globalSettings.fmsBridge.summary.lastFailed') }}</span>
          <strong>{{ overview.latest_run?.summary.failed ?? 0 }}</strong>
        </div>
      </section>

      <section class="fms-bridge__mirror-list">
        <div class="fms-bridge__section-heading">
          <div>
            <h3>{{ t('system.globalSettings.fmsBridge.mirrors.title') }}</h3>
            <p>{{ t('system.globalSettings.fmsBridge.mirrors.description') }}</p>
          </div>
          <span v-if="overview.latest_run?.finished_at" class="fms-bridge__updated">
            <t-icon name="time" />{{ formatTime(overview.latest_run.finished_at) }}
          </span>
        </div>
        <t-empty v-if="overview.mirrors.length === 0" :description="t('system.globalSettings.fmsBridge.mirrors.empty')" />
        <div v-else class="data-table-shell fms-bridge__table-shell">
          <t-table class="fms-bridge__table" row-key="id" :data="overview.mirrors" :columns="columns" hover @row-click="openDetail">
            <template #book="{ row }">
              <div class="fms-bridge__book">
                <strong>{{ row.book.title || t('system.globalSettings.fmsBridge.mirrors.untitled') }}</strong>
                <span>{{ row.book.book_no || row.book.subject || '—' }}</span>
              </div>
            </template>
            <template #readiness_status="{ row }">
              <t-tag :theme="row.handoff_eligible ? 'success' : 'warning'" size="small" variant="light-outline">
                {{ row.readiness_status || '—' }}
              </t-tag>
            </template>
            <template #retrieval_unit_count="{ row }"><span>{{ row.retrieval_unit_count }}</span></template>
            <template #last_synced_at="{ row }"><span class="fms-bridge__time">{{ formatTime(row.last_synced_at) }}</span></template>
            <template #action="{ row }">
              <div class="fms-bridge__table-actions">
                <t-button variant="text" size="small" @click.stop="openDetail(row)">{{ t('system.globalSettings.fmsBridge.mirrors.inspect') }}</t-button>
                <t-button variant="text" size="small" :loading="retryingMirrorId === row.id" :disabled="Boolean(retryingMirrorId)" @click.stop="retryMirror(row)">
                  {{ t('system.globalSettings.fmsBridge.actions.retryMirror') }}
                </t-button>
              </div>
            </template>
          </t-table>
        </div>
      </section>
      <t-alert v-if="(overview.latest_run?.summary.failed ?? 0) > 0" theme="warning" class="fms-bridge__alert">
        <template #message>
          <strong>{{ t('system.globalSettings.fmsBridge.summary.failureDetails') }}</strong>
          <ul v-if="failureEntries.length" class="fms-bridge__failures">
            <li v-for="failure in failureEntries" :key="failure[0]"><span class="mono">{{ failure[0] }}</span>: {{ failure[1] }}</li>
          </ul>
        </template>
      </t-alert>
    </template>

    <SettingDrawer
      v-model:visible="detailVisible"
      :title="selectedMirror?.book.title || t('system.globalSettings.fmsBridge.detail.title')"
      :description="selectedMirror?.source_ref"
      icon="file-paste"
      width="720px"
      :min-width="520"
      :max-width="980"
      storage-key="setting-drawer:width:fms-bridge-detail"
      hide-footer
    >
      <div v-if="detailLoading" class="fms-bridge__drawer-loading"><t-loading size="small" /></div>
      <t-alert v-else-if="detailError" theme="error" :message="detailError" />
      <template v-else-if="selectedMirror">
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ t('system.globalSettings.fmsBridge.detail.source') }}</h4>
          <dl class="fms-bridge__fields">
            <div><dt>{{ t('system.globalSettings.fmsBridge.detail.bookNo') }}</dt><dd>{{ selectedMirror.book.book_no || '—' }}</dd></div>
            <div><dt>{{ t('system.globalSettings.fmsBridge.detail.subject') }}</dt><dd>{{ selectedMirror.book.subject || '—' }}</dd></div>
            <div><dt>{{ t('system.globalSettings.fmsBridge.detail.sourceRef') }}</dt><dd class="mono">{{ selectedMirror.source_ref }}</dd></div>
            <div><dt>{{ t('system.globalSettings.fmsBridge.detail.revision') }}</dt><dd class="mono">{{ selectedMirror.revision_key }}</dd></div>
          </dl>
        </section>
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ t('system.globalSettings.fmsBridge.detail.artifacts') }}</h4>
          <div class="fms-bridge__artifacts">
            <div v-for="artifact in selectedMirror.artifacts" :key="artifact.kind" class="fms-bridge__artifact">
              <div><strong>{{ artifact.kind }}</strong><span>{{ artifact.record_count ?? '—' }}</span></div>
              <t-tag :theme="artifact.available ? 'success' : artifact.required ? 'danger' : 'default'" size="small" variant="light-outline">
                {{ artifact.available ? t('system.globalSettings.fmsBridge.detail.available') : t('system.globalSettings.fmsBridge.detail.unavailable') }}
              </t-tag>
            </div>
          </div>
        </section>
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ t('system.globalSettings.fmsBridge.detail.units') }}</h4>
          <p class="fms-bridge__drawer-note">{{ t('system.globalSettings.fmsBridge.detail.unitsDescription') }}</p>
          <div v-if="selectedMirror.retrieval_units.length === 0" class="fms-bridge__drawer-empty">—</div>
          <article v-for="unit in selectedMirror.retrieval_units" :key="unit.unit_id" class="fms-bridge__unit">
            <div class="fms-bridge__unit-meta"><span>#{{ unit.ordinal }}</span><span>{{ unit.content_type }}</span><span class="mono">{{ unit.structural_node_id }}</span></div>
            <p>{{ previewText(unit.content_text) }}</p>
            <code v-if="Object.keys(unit.locator || {}).length">{{ JSON.stringify(unit.locator) }}</code>
          </article>
        </section>
      </template>
    </SettingDrawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getFMSBridgeMirrorDetail,
  getFMSBridgeOverview,
  triggerFMSBridgeMirrorReconcile,
  triggerFMSBridgeReconcile,
  type FMSBridgeOverview,
  type FMSMirrorDetail,
  type FMSMirrorSummary,
} from '@/api/system'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'

const { t } = useI18n()
const loading = ref(false)
const syncing = ref(false)
const retryingMirrorId = ref('')
const error = ref('')
const overview = ref<FMSBridgeOverview>()
const detailVisible = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
const selectedMirror = ref<FMSMirrorDetail>()

const columns = computed(() => [
  { colKey: 'book', title: t('system.globalSettings.fmsBridge.mirrors.columns.book'), width: 250 },
  { colKey: 'readiness_status', title: t('system.globalSettings.fmsBridge.mirrors.columns.readiness'), width: 150 },
  { colKey: 'retrieval_unit_count', title: t('system.globalSettings.fmsBridge.mirrors.columns.units'), width: 120 },
  { colKey: 'last_synced_at', title: t('system.globalSettings.fmsBridge.mirrors.columns.syncedAt'), width: 180 },
  { colKey: 'action', title: '', width: 172, fixed: 'right' as const },
])

const reload = async () => {
  loading.value = true
  error.value = ''
  try {
    overview.value = await getFMSBridgeOverview()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('system.globalSettings.fmsBridge.errors.load')
  } finally {
    loading.value = false
  }
}

const startSync = async () => {
  syncing.value = true
  try {
    await triggerFMSBridgeReconcile()
    MessagePlugin.success(t('system.globalSettings.fmsBridge.messages.queued'))
    await reload()
  } catch (cause) {
    MessagePlugin.error(cause instanceof Error ? cause.message : t('system.globalSettings.fmsBridge.errors.sync'))
  } finally {
    syncing.value = false
  }
}

const failureEntries = computed(() => Object.entries(overview.value?.latest_run?.summary.failures ?? {}).slice(0, 10))

const retryMirror = async (row: FMSMirrorSummary) => {
  retryingMirrorId.value = row.id
  try {
    await triggerFMSBridgeMirrorReconcile(row.id)
    MessagePlugin.success(t('system.globalSettings.fmsBridge.messages.mirrorQueued'))
    await reload()
  } catch (cause) {
    MessagePlugin.error(cause instanceof Error ? cause.message : t('system.globalSettings.fmsBridge.errors.mirrorSync'))
  } finally {
    retryingMirrorId.value = ''
  }
}

const openDetail = async (source: FMSMirrorSummary | { row: FMSMirrorSummary }) => {
  const row = 'row' in source ? source.row : source
  detailVisible.value = true
  detailLoading.value = true
  detailError.value = ''
  selectedMirror.value = undefined
  try {
    selectedMirror.value = await getFMSBridgeMirrorDetail(row.id)
  } catch (cause) {
    detailError.value = cause instanceof Error ? cause.message : t('system.globalSettings.fmsBridge.errors.detail')
  } finally {
    detailLoading.value = false
  }
}

const runStatus = (status?: string) => status || t('system.globalSettings.fmsBridge.summary.noRun')
const statusClass = (status?: string) => status ? `fms-bridge__run--${status}` : ''
const formatTime = (value?: string) => value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—'
const previewText = (value: string) => value.length > 500 ? `${value.slice(0, 500)}…` : value

onMounted(() => { void reload() })
</script>

<style scoped>
.fms-bridge { min-height: 100%; padding: 4px 2px 24px; }
.fms-bridge__header, .fms-bridge__section-heading, .fms-bridge__actions { display: flex; align-items: center; }
.fms-bridge__header, .fms-bridge__section-heading { justify-content: space-between; gap: 20px; }
.fms-bridge__header h2, .fms-bridge__section-heading h3 { margin: 0; }
.fms-bridge__section-heading p, .fms-bridge__drawer-note { margin: 6px 0 0; color: var(--td-text-color-secondary); font-size: 13px; }
.fms-bridge__actions { gap: 10px; }
.fms-bridge__loading { display: grid; gap: 20px; padding-top: 24px; }
.fms-bridge__alert { margin: 18px 0; }
.fms-bridge__alert p { margin: 5px 0 0; }
.fms-bridge__failures { margin: 8px 0 0; padding-left: 20px; max-height: 180px; overflow: auto; }
.fms-bridge__failures li { margin: 4px 0; overflow-wrap: anywhere; }
.fms-bridge__metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin: 20px 0 26px; }
.fms-bridge__metric { min-height: 92px; padding: 16px; border: 1px solid var(--td-component-border); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); display: flex; flex-direction: column; justify-content: space-between; }
.fms-bridge__metric span { color: var(--td-text-color-secondary); font-size: 13px; }
.fms-bridge__metric strong { font-size: 24px; line-height: 1.2; }
.fms-bridge__metric--warning strong, .fms-bridge__run--failed { color: var(--td-warning-color); }
.fms-bridge__run--completed { color: var(--td-success-color); }
.fms-bridge__mirror-list { padding-top: 4px; }
.fms-bridge__updated { color: var(--td-text-color-secondary); font-size: 13px; display: inline-flex; gap: 5px; align-items: center; }
.fms-bridge__table-shell { max-width: 100%; overflow-x: auto; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-lg); background: var(--td-bg-color-container); }
.fms-bridge__table-shell :deep(.t-table) { min-width: 872px; }
.fms-bridge__table-shell :deep(.t-table th), .fms-bridge__table-shell :deep(.t-table td) { vertical-align: middle; }
.fms-bridge__table-actions { display: inline-flex; align-items: center; gap: 12px; min-width: 150px; white-space: nowrap; }
.fms-bridge__book { display: grid; gap: 3px; min-width: 0; }
.fms-bridge__book strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fms-bridge__book span, .fms-bridge__time { color: var(--td-text-color-secondary); font-size: 12px; }
.fms-bridge__time { white-space: nowrap; }
.fms-bridge__drawer-loading, .fms-bridge__drawer-empty { padding: 30px; text-align: center; color: var(--td-text-color-secondary); }
.fms-bridge__fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; margin: 0; }
.fms-bridge__fields div { min-width: 0; }
.fms-bridge__fields dt { color: var(--td-text-color-secondary); font-size: 12px; margin-bottom: 4px; }
.fms-bridge__fields dd { margin: 0; overflow-wrap: anywhere; }
.fms-bridge__artifacts { display: grid; gap: 8px; }
.fms-bridge__artifact { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 9px 10px; border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); }
.fms-bridge__artifact div { display: flex; gap: 8px; align-items: baseline; }
.fms-bridge__artifact span { color: var(--td-text-color-secondary); font-size: 12px; }
.fms-bridge__unit { margin-top: 10px; padding: 12px; border: 1px solid var(--td-component-border); border-radius: var(--td-radius-small); }
.fms-bridge__unit-meta { display: flex; flex-wrap: wrap; gap: 8px; color: var(--td-text-color-secondary); font-size: 12px; }
.fms-bridge__unit p { margin: 8px 0; white-space: pre-wrap; line-height: 1.6; }
.fms-bridge__unit code { display: block; overflow-wrap: anywhere; color: var(--td-text-color-secondary); font-size: 12px; }
@media (max-width: 860px) { .fms-bridge__metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
