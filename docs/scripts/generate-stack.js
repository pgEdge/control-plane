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
        "⚠️ Please paste the output from `docker node ls --format '{{ .ID }} {{ .ManagerStatus }}'`";
      return;
    }

    const nodes = raw.split(/\n+/).
      filter(l => l.trim()).
      map(l => l.split(/\s+/)).
      map(([id, managerStatus]) => ({ id, managerStatus }));

    if (nodes.length === 0) {
      outputEl.textContent = "⚠️ No nodes found in input.";
      return;
    }

    let yaml = "services:";

    nodes.forEach((node, i) => {
      const hostID = `host-${i + 1}`;

      let envVars = [
        `PGEDGE_HOST_ID=${hostID}`,
        "PGEDGE_DATA_DIR=/data/pgedge/control-plane",
      ];

      if (!node.managerStatus) {
        envVars.push("PGEDGE_ETCD_MODE=client");
      }

      const environment = envVars.join("\n      - ");
      
      yaml += `
  ${hostID}:
    image: ghcr.io/pgedge/control-plane:v0.4.0
    command: run
    environment:
      - ${environment}
    volumes:
      - /data/pgedge/control-plane:/data/pgedge/control-plane
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - host
    deploy:
      placement:
        constraints:
          - node.id==${node.id}`;
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
