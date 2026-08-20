import { useEffect, useState } from "react";

const hoverInputQuery = "(hover: hover) and (pointer: fine)";

export function useHasHoverInput() {
  const [hasHover, setHasHover] = useState(() =>
    typeof window === "undefined" ? true : window.matchMedia(hoverInputQuery).matches,
  );

  useEffect(() => {
    const media = window.matchMedia(hoverInputQuery);
    const update = () => setHasHover(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  return hasHover;
}
