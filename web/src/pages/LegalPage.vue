<template>
  <q-page class="legal-page q-pa-md">
    <div class="legal-content" v-html="renderedMarkdown" />
  </q-page>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { TERMS_OF_SERVICE } from 'src/assets/legal/terms'
import { PRIVACY_POLICY } from 'src/assets/legal/privacy'

const route = useRoute()

const markdownSource = computed(() => {
  if (route.path.includes('privacy')) return PRIVACY_POLICY
  return TERMS_OF_SERVICE
})

const renderedMarkdown = computed(() => {
  // Sanitize with DOMPurify after converting Markdown to HTML to prevent XSS.
  return DOMPurify.sanitize(marked.parse(markdownSource.value) as string)
})
</script>

<style lang="scss">
.legal-page {
  max-width: 800px;
  margin: 0 auto;
}

.legal-content {
  color: rgba(255, 255, 255, 0.87);
  line-height: 1.7;
  font-size: 0.95rem;

  h1 {
    font-size: 1.75rem;
    font-weight: 700;
    margin-bottom: 0.5rem;
    color: #fff;
    border-bottom: 1px solid rgba(255, 255, 255, 0.12);
    padding-bottom: 0.5rem;
  }

  h2 {
    font-size: 1.25rem;
    font-weight: 600;
    margin-top: 2rem;
    margin-bottom: 0.75rem;
    color: #fff;
  }

  h3 {
    font-size: 1.05rem;
    font-weight: 600;
    margin-top: 1.5rem;
    margin-bottom: 0.5rem;
    color: rgba(255, 255, 255, 0.9);
  }

  p {
    margin-bottom: 1rem;
  }

  ul,
  ol {
    margin-bottom: 1rem;
    padding-left: 1.5rem;
  }

  li {
    margin-bottom: 0.4rem;
  }

  strong {
    color: #fff;
    font-weight: 600;
  }

  em {
    color: rgba(255, 255, 255, 0.6);
    font-style: italic;
  }

  hr {
    border: none;
    border-top: 1px solid rgba(255, 255, 255, 0.12);
    margin: 2rem 0;
  }

  a {
    color: var(--q-primary);
    text-decoration: none;

    &:hover {
      text-decoration: underline;
    }
  }

  blockquote {
    border-left: 3px solid var(--q-primary);
    margin: 1rem 0;
    padding: 0.5rem 1rem;
    color: rgba(255, 255, 255, 0.6);
  }

  code {
    background: rgba(255, 255, 255, 0.08);
    border-radius: 3px;
    padding: 0.1rem 0.4rem;
    font-family: monospace;
    font-size: 0.9em;
  }
}
</style>
