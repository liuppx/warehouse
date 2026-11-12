export const routes = [
  {
    path: '/share/:token',
    name: 'public-share',
    component: () => import('@/views/share/PublicDirectoryShare.vue')
  },
  {
    path: '/',
    component: () => import('@/components/layout/AppLayout.vue'),
    children: [
      {
        path: '',
        name: 'home',
        component: () => import('@/views/home/Index.vue')
      }
    ]
  }
]
