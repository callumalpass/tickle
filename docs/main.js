const flowNodes = Array.from(document.querySelectorAll("[data-flow-node]"));
const loopSteps = Array.from(document.querySelectorAll(".loop-step"));

let activeIndex = 0;

function setActive(index) {
  flowNodes.forEach((node, nodeIndex) => {
    node.classList.toggle("is-active", nodeIndex === index);
  });
  loopSteps.forEach((step, stepIndex) => {
    step.classList.toggle("is-current", stepIndex === index % loopSteps.length);
  });
}

setActive(activeIndex);

if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
  window.setInterval(() => {
    activeIndex = (activeIndex + 1) % flowNodes.length;
    setActive(activeIndex);
  }, 1600);
}
