const copyButtons = document.querySelectorAll("[data-copy]");

copyButtons.forEach((button) => {
  const originalText = button.textContent;

  button.addEventListener("click", async () => {
    const value = button.getAttribute("data-copy") || "";

    try {
      await navigator.clipboard.writeText(value);
      button.textContent = "Copied";
    } catch {
      button.textContent = "Select command";
    }

    window.setTimeout(() => {
      button.textContent = originalText;
    }, 1800);
  });
});
