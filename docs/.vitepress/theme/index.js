import DefaultTheme from 'vitepress/theme'
import SwaggerUI from './components/SwaggerUI.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('SwaggerUI', SwaggerUI)
  },
}
