<template>
  <q-layout view="hHh lpr fFf">
    <q-header class="bg-dark" elevated>
      <q-toolbar>
        <router-link to="/" class="flex items-center no-wrap text-decoration-none">
          <img :src="logoUrl" alt="RadioLedger" style="height: 36px;" />
        </router-link>
      </q-toolbar>
    </q-header>
    <q-page-container>
      <router-view />
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import logoUrl from 'src/assets/branding/logo-navbar.png'
</script>

<style scoped>
a.text-decoration-none {
  text-decoration: none;
}
</style>
