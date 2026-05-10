<script lang="ts">
  let { children } = $props();
</script>

<main>
  {@render children()}
</main>

<style>
  :global(body) {
    margin: 0;
    font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
    background: linear-gradient(160deg, #0d1b2a 0%, #1b263b 100%);
    color: #e0e1dd;
    min-height: 100vh;
  }
  main {
    max-width: 720px;
    margin: 0 auto;
    padding: 2rem 1rem;
  }
</style>
