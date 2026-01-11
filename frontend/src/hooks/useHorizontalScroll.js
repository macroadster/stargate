import { useRef, useEffect, useState } from 'react';

export const useHorizontalScroll = () => {
  const elRef = useRef();
  const [isDragging, setIsDragging] = useState(false);

  useEffect(() => {
    const el = elRef.current;
    if (el) {
      const onWheel = (e) => {
        if (e.deltaY === 0) return;
        e.preventDefault();
        el.scrollTo({
          left: el.scrollLeft + e.deltaY,
          behavior: 'smooth',
        });
      };

      let isDown = false;
      let startX;
      let scrollLeft;

      const onMouseDown = (e) => {
        isDown = true;
        setIsDragging(false);
        el.classList.add('active');
        startX = e.pageX - el.offsetLeft;
        scrollLeft = el.scrollLeft;
      };

      const onMouseLeave = () => {
        isDown = false;
        el.classList.remove('active');
      };

      const onMouseUp = () => {
        isDown = false;
        el.classList.remove('active');
        setTimeout(() => setIsDragging(false), 0);
      };

      const onMouseMove = (e) => {
        if (!isDown) return;
        e.preventDefault();
        setIsDragging(true);
        const x = e.pageX - el.offsetLeft;
        const walk = (x - startX) * 3; //scroll-fast
        el.scrollLeft = scrollLeft - walk;
      };

      el.addEventListener('wheel', onWheel);
      el.addEventListener('mousedown', onMouseDown);
      el.addEventListener('mouseleave', onMouseLeave);
      el.addEventListener('mouseup', onMouseUp);
      el.addEventListener('mousemove', onMouseMove);

      return () => {
        el.removeEventListener('wheel', onWheel);
        el.removeEventListener('mousedown', onMouseDown);
        el.removeEventListener('mouseleave', onMouseLeave);
        el.removeEventListener('mouseup', onMouseUp);
        el.removeEventListener('mousemove', onMouseMove);
      };
    }
  }, []);
  return { elRef, isDragging };
};
