document.querySelectorAll(".yaml-generate").forEach((btn) => {
  btn.addEventListener("click", () => {
    const inputId = btn.dataset.input;
    const outputId = btn.dataset.output;

    const inputEl = document.getElementById(inputId);
    let outputEl = document.getElementById(outputId);

    const codeBlock = outputEl && outputEl.querySelector("code");
    if (codeBlock) outputEl = codeBlock;

    if (!inputEl || !outputEl) return;

    const raw = inputEl.value.trim();
    outputEl.textContent = "";

    if (!raw) {
      outputEl.textContent =
        "⚠️ Please paste the output from `docker node ls --format '{{ .ID }}'`";
      return;
    }

    const lines = raw.split(/\n+/).filter((l) => l.trim());
    const nodes = lines.map((id) => id.trim());

    if (nodes.length === 0) {
      outputEl.textContent = "⚠️ No nodes found in input.";
      return;
    }

    let yaml = "services:";

    nodes.forEach((node, i) => {
      const hostID = `host-${i + 1}`;
      yaml += `
  ${hostID}:
    image: ghcr.io/pgedge/control-plane:v0.4.0
    command: run
    environment:
      - PGEDGE_HOST_ID=${hostID}
      - PGEDGE_DATA_DIR=/data/pgedge/control-plane
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==${node}`;
    });

    yaml += `
networks:
  host:
    name: host
    external: true
`;

    outputEl.textContent = yaml;
  });
});
