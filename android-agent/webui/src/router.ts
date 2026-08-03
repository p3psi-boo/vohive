import { createRouter, createWebHashHistory } from 'vue-router'
import { restoreSession, session } from './session'

// hash 路由：所有路径都是 /#/...，LocalHttpServer 无需 fallback。
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./views/Login.vue') },
    { path: '/', name: 'home', component: () => import('./views/Home.vue'), meta: { requiresAuth: true } },
    { path: '/settings', name: 'settings', component: () => import('./views/Settings.vue'), meta: { requiresAuth: true } }
  ]
})

router.beforeEach(async (to) => {
  if (!session.ready) await restoreSession()
  if (to.meta.requiresAuth && !session.authenticated) return { name: 'login' }
  if (to.name === 'login' && session.authenticated) return { name: 'home' }
})

export default router
