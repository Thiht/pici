<script setup>
import { onMounted, ref } from 'vue'
import { withBase } from 'vitepress'
import bundleUrl from 'swagger-ui-dist/swagger-ui-bundle.js?url'
import presetUrl from 'swagger-ui-dist/swagger-ui-standalone-preset.js?url'
import 'swagger-ui-dist/swagger-ui.css'

const el = ref(null)

onMounted(() => {
  const loadScript = (src) =>
    new Promise((resolve, reject) => {
      const s = document.createElement('script')
      s.src = src
      s.onload = resolve
      s.onerror = reject
      document.head.appendChild(s)
    })

  loadScript(bundleUrl)
    .then(() => loadScript(presetUrl))
    .then(() => {
      const { SwaggerUIBundle, SwaggerUIStandalonePreset } = window
      SwaggerUIBundle({
        domNode: el.value,
        url: withBase('/openapi.yaml'),
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        plugins: [SwaggerUIBundle.plugins.DownloadUrl],
        layout: 'StandaloneLayout',
      })
    })
})
</script>

<template>
  <div ref="el" class="swagger-ui" />
</template>

<style>
/* Let the Swagger UI fill the whole content area on its page.
   Scoped with :has() so it only affects pages that embed this component. */
.VPDoc:has(.swagger-ui) .content-container {
  max-width: none;
}

.VPContent.has-sidebar:has(.swagger-ui) {
  padding-right: 0;
}
</style>
