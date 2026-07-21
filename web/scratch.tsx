import React from 'react';
import { useForm } from 'react-hook-form';

export function Test() {
  const form = useForm({ defaultValues: { text: '' } });
  const onSubmit = (values: any) => console.log('submitted', values);
  return (
    <form onSubmit={form.handleSubmit(onSubmit)}>
      <input {...form.register('text')} onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          form.handleSubmit(onSubmit)();
        }
      }} />
    </form>
  );
}
