import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'
import './assets/styles/main.css'

const app = createApp(App)

// Pinia
const pinia = createPinia()
app.use(pinia)

// i18n
const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('locale') || 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS
  }
})
app.use(i18n)

// Element Plus
app.use(ElementPlus)

// Element Plus Icons
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

// Router
app.use(router)

app.mount('#app')
