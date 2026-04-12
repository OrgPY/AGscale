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

const docCodeBlocks = document.querySelectorAll(".doc-content pre");

docCodeBlocks.forEach((block) => {
  const code = block.querySelector("code");
  if (!code) {
    return;
  }

  const button = document.createElement("button");
  button.type = "button";
  button.className = "doc-code-copy";
  button.textContent = "Copy";
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(code.textContent || "");
      button.textContent = "Copied";
    } catch {
      button.textContent = "Select";
    }

    window.setTimeout(() => {
      button.textContent = "Copy";
    }, 1600);
  });

  block.appendChild(button);
});
