import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@/styles.css'
import '@/theme'
import App from './App.vue'
import { elementLocale } from '@/locale'

createApp(App).use(ElementPlus, { locale: elementLocale }).mount('#app')
